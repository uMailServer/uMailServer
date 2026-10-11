package imap

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// F6600: per-session message-sequence view (RFC 3501 §7.4.1). Sequence
// numbers are positions in a list of UIDs that the session keeps. The mailstore
// addresses by *live* position, which shifts when another session expunges, so
// the session translates at its boundary and only renumbers (announcing
// EXPUNGE / EXISTS) at points where the protocol allows it: command completion
// of anything but FETCH / STORE / SEARCH (and their UID forms), and IDLE.

// uidLister is the optional mailstore primitive that returns the mailbox's
// UIDs in sequence order. Stores without it keep live numbering.
type uidLister interface {
	MailboxUIDs(user, mailbox string) ([]uint32, error)
}

// MailboxUIDs returns the UIDs of a mailbox in ascending (sequence) order.
func (m *BboltMailstore) MailboxUIDs(user, mailbox string) ([]uint32, error) {
	return m.db.GetMessageUIDs(user, mailbox)
}

func (s *Session) lister() (uidLister, bool) {
	if s.server == nil || s.server.mailstore == nil {
		return nil, false
	}
	l, ok := s.server.mailstore.(uidLister)
	return l, ok
}

// snapInit takes the session's sequence snapshot of the selected mailbox.
func (s *Session) snapInit() {
	s.snap, s.snapOn, s.syncOnTag = nil, false, false
	if s.selected == nil {
		return
	}
	if l, ok := s.lister(); ok {
		if u, err := l.MailboxUIDs(s.user, s.selected.Name); err == nil {
			s.snap, s.snapOn = u, true
			s.selected.Exists = len(u)
		}
	}
}

// snapSeq returns the session sequence number of uid.
func (s *Session) snapSeq(uid uint32) (uint32, bool) {
	i := sort.Search(len(s.snap), func(i int) bool { return s.snap[i] >= uid })
	if i < len(s.snap) && s.snap[i] == uid {
		return uint32(i + 1), true
	}
	return 0, false
}

func (s *Session) liveUIDs() ([]uint32, error) {
	l, _ := s.lister()
	return l.MailboxUIDs(s.user, s.selected.Name)
}

// seqToLive translates a session sequence-set into the live sequence-set the
// mailstore understands. Messages expunged by others are dropped. An empty
// result means "nothing matches".
func (s *Session) seqToLive(set string) (string, error) {
	if !s.snapOn {
		return set, nil
	}
	ranges, err := ParseSequenceSet(set)
	if err != nil {
		return "", err
	}
	if len(ranges) == 0 {
		return "", nil
	}
	live, err := s.liveUIDs()
	if err != nil {
		return "", err
	}
	liveSeq := make(map[uint32]uint32, len(live))
	for i, u := range live {
		liveSeq[u] = uint32(i + 1)
	}
	total := uint32(len(s.snap))
	var out []string
	for i, uid := range s.snap {
		seq := uint32(i + 1)
		for _, r := range ranges {
			if r.Contains(seq, total) {
				if ls, ok := liveSeq[uid]; ok {
					out = append(out, strconv.FormatUint(uint64(ls), 10))
				}
				break
			}
		}
	}
	return strings.Join(out, ","), nil
}

// fetchSel fetches by session sequence-set and returns messages numbered in the
// session's view.
func (s *Session) fetchSel(set string, items []string) ([]*Message, error) {
	live, err := s.seqToLive(set)
	if err != nil {
		return nil, err
	}
	msgs, err := s.server.mailstore.FetchMessages(s.user, s.selected.Name, live, items)
	if err != nil || !s.snapOn {
		return msgs, err
	}
	out := msgs[:0]
	for _, m := range msgs {
		if sq, ok := s.snapSeq(m.UID); ok {
			m.SeqNum = sq
			out = append(out, m)
		}
	}
	return out, nil
}

func (s *Session) storeSel(set string, flags []string, op FlagOperation) error {
	live, err := s.seqToLive(set)
	if err != nil {
		return err
	}
	return s.server.mailstore.StoreFlags(s.user, s.selected.Name, live, flags, op)
}

// liveSeqToSession maps live sequence numbers (from the mailstore) to the
// session's numbering, dropping messages the session has not been told about.
func (s *Session) liveSeqsToSession(seqs []uint32) []uint32 {
	if !s.snapOn || len(seqs) == 0 {
		return seqs
	}
	live, err := s.liveUIDs()
	if err != nil {
		return seqs
	}
	out := make([]uint32, 0, len(seqs))
	for _, q := range seqs {
		if q == 0 || int(q) > len(live) {
			continue
		}
		if sq, ok := s.snapSeq(live[q-1]); ok {
			out = append(out, sq)
		}
	}
	return out
}

// syncSelected announces what other sessions did: EXPUNGE for messages gone
// (highest number first so each number stays valid), then EXISTS / RECENT for
// new ones, and adopts the live state as the new snapshot.
func (s *Session) syncSelected() {
	if !s.snapOn || s.selected == nil || s.State() != StateSelected {
		return
	}
	live, err := s.liveUIDs()
	if err != nil {
		return
	}
	liveSet := make(map[uint32]bool, len(live))
	for _, u := range live {
		liveSet[u] = true
	}
	var gone []int
	kept := make([]uint32, 0, len(s.snap))
	for i, u := range s.snap {
		if liveSet[u] {
			kept = append(kept, u)
		} else {
			gone = append(gone, i)
		}
	}
	for k := len(gone) - 1; k >= 0; k-- {
		s.WriteData(fmt.Sprintf("%d EXPUNGE", gone[k]+1))
	}
	have := make(map[uint32]bool, len(kept))
	for _, u := range kept {
		have[u] = true
	}
	// F6905: \Recent messages that other sessions expunged no longer count;
	// RECENT can never exceed the messages that remain.
	if s.selected.Recent > len(kept) {
		s.selected.Recent = len(kept)
	}
	added := 0
	for _, u := range live {
		if !have[u] {
			kept = append(kept, u)
			added++
		}
	}
	s.snap = kept
	s.selected.Exists = len(kept)
	if added > 0 {
		s.selected.Recent += added
		s.WriteData(fmt.Sprintf("%d EXISTS", len(kept)))
		s.WriteData(fmt.Sprintf("%d RECENT", s.selected.Recent))
	}
}

// syncAllowed reports whether untagged EXPUNGE may follow the command
// (RFC 3501 §7.4.1 forbids it during FETCH, STORE and SEARCH).
func syncAllowed(command string, args []string) bool {
	switch command {
	case "FETCH", "STORE", "SEARCH", "SORT", "THREAD",
		"SELECT", "EXAMINE", "CLOSE", "UNSELECT", "LOGOUT", "STARTTLS", "COMPRESS":
		return false
	case "UID":
		if len(args) > 0 {
			switch strings.ToUpper(args[0]) {
			case "FETCH", "STORE", "SEARCH", "SORT", "THREAD":
				return false
			}
		}
	}
	return true
}

// seqSetInRange reports whether every message sequence number named in a
// (non-UID) sequence-set refers to an existing message (RFC 3501 §6.4.8:
// a message sequence number out of range is an error). Range ends beyond the
// count are clamped like "*"; a lone number or range start above the count,
// or 0, is not in range. An empty mailbox accepts only "*" forms.
func (s *Session) seqSetInRange(set string) bool {
	ranges, err := ParseSequenceSet(set)
	if err != nil {
		return true // reported by the normal path
	}
	if !s.snapOn {
		return true // no authoritative message count for this store
	}
	total := uint32(len(s.snap))
	for _, r := range ranges {
		if r.Start == 0 || r.End == 0 {
			continue // "*" form: clamps to the last message
		}
		lo := r.Start
		if r.End < lo {
			lo = r.End
		}
		if lo > total {
			return false
		}
	}
	return true
}
