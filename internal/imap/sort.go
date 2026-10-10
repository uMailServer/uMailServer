package imap

import (
	"bufio"
	"bytes"
	"fmt"
	"mime"
	"net/mail"
	"net/textproto"
	"sort"
	"strings"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

// SortCriterion represents a sort criteria per RFC 5256
type SortCriterion struct {
	Field      string // ARRIVAL, DATE, FROM, SUBJECT, SIZE, UID
	Descending bool
}

// SortResult represents the result of a SORT command
type SortResult struct {
	SequenceNumbers []uint32
}

// parseSortCriteria parses SORT criteria from args
// Example: ["ARRIVAL", "REVERSE", "SUBJECT"]
func parseSortCriteria(args []string) ([]SortCriterion, error) {
	var criteria []SortCriterion
	// RFC 5256 §3: criteria sort ascending; REVERSE makes only the next
	// criterion descending. F5491: the default was descending, so SORT
	// (DATE) listed newest first and REVERSE DATE oldest first.
	descending := false

	for i := 0; i < len(args); i++ {
		arg := strings.ToUpper(args[i])
		switch arg {
		case "ARRIVAL":
			criteria = append(criteria, SortCriterion{Field: "ARRIVAL", Descending: descending})
			descending = false // reset after each criterion
		case "DATE":
			criteria = append(criteria, SortCriterion{Field: "DATE", Descending: descending})
			descending = false
		case "FROM":
			criteria = append(criteria, SortCriterion{Field: "FROM", Descending: descending})
			descending = false
		case "SUBJECT":
			criteria = append(criteria, SortCriterion{Field: "SUBJECT", Descending: descending})
			descending = false
		case "SIZE":
			criteria = append(criteria, SortCriterion{Field: "SIZE", Descending: descending})
			descending = false
		case "UID":
			criteria = append(criteria, SortCriterion{Field: "UID", Descending: descending})
			descending = false
		case "REVERSE":
			descending = true // applies to the next criterion
		case "SCORE":
			// NOTREVEALED - for threading, not supported in basic sort
			return nil, fmt.Errorf("unsupported sort criterion: SCORE")
		case "CC":
			criteria = append(criteria, SortCriterion{Field: "CC", Descending: descending})
			descending = false
		case "TO":
			criteria = append(criteria, SortCriterion{Field: "TO", Descending: descending})
			descending = false
		default:
			return nil, fmt.Errorf("unknown sort criterion: %s", arg)
		}
	}

	if len(criteria) == 0 {
		return nil, fmt.Errorf("no sort criteria provided")
	}

	return criteria, nil
}

// messageForSort is internal helper for sorting
type messageForSort struct {
	seqNum  uint32
	uid     uint32
	date    time.Time
	from    string
	to      string
	cc      string
	subject string
	size    int64
	arrival time.Time
}

// sortMessagesByCriteria sorts messages according to RFC 5256
func sortMessagesByCriteria(messages []*storage.MessageMetadata, criteria []SortCriterion, seqNums []uint32) []uint32 {
	return sortMessagesByKeys(messages, nil, criteria, seqNums)
}

// sortMessagesByKeys sorts like sortMessagesByCriteria; ccs[i], when ccs is
// non-nil, is message i's Cc header (the metadata does not carry it).
// F5561: CC and TO compared nothing, FROM compared the raw header instead
// of the first address's addr-mailbox, and a missing or unparsable Date
// sorted as the zero time instead of the INTERNALDATE (RFC 5256 §2.2, §3).
func sortMessagesByKeys(messages []*storage.MessageMetadata, ccs []string, criteria []SortCriterion, seqNums []uint32) []uint32 {
	if len(messages) == 0 {
		return nil
	}

	// Build sortable list
	sortable := make([]messageForSort, len(messages))
	for i, msg := range messages {
		sortable[i] = messageForSort{
			seqNum:  seqNums[i],
			uid:     msg.UID,
			date:    sentDate(msg.Date, msg.InternalDate),
			from:    strings.ToLower(addrMailbox(msg.From)),
			to:      strings.ToLower(addrMailbox(msg.To)),
			subject: baseSubject(msg.Subject),
			size:    msg.Size,
			arrival: msg.InternalDate,
		}
		if ccs != nil {
			sortable[i].cc = strings.ToLower(addrMailbox(ccs[i]))
		}
	}

	// Compare criteria in order, preserving mailbox order when all keys tie.
	sort.SliceStable(sortable, func(i, j int) bool {
		for _, c := range criteria {
			ascending := func(a, b int) bool {
				switch c.Field {
				case "ARRIVAL":
					return sortable[a].arrival.Before(sortable[b].arrival)
				case "DATE":
					return sortable[a].date.Before(sortable[b].date)
				case "FROM":
					return sortable[a].from < sortable[b].from
				case "TO":
					return sortable[a].to < sortable[b].to
				case "CC":
					return sortable[a].cc < sortable[b].cc
				case "SUBJECT":
					return sortable[a].subject < sortable[b].subject
				case "SIZE":
					return sortable[a].size < sortable[b].size
				case "UID":
					return sortable[a].uid < sortable[b].uid
				}
				return false
			}
			a, b := i, j
			if c.Descending {
				a, b = b, a
			}
			if ascending(a, b) {
				return true
			}
			if ascending(b, a) {
				return false
			}
		}
		return false
	})

	// Extract sequence numbers
	result := make([]uint32, len(sortable))
	for i, msg := range sortable {
		result[i] = msg.seqNum
	}

	return result
}

// baseSubject returns the RFC 5256 §2.1 base subject used by SORT SUBJECT:
// decoded, whitespace-collapsed and case-folded, with leading "Re:",
// "Fw:", "Fwd:" and "[blob]" prefixes, trailing "(fwd)" and the
// "[fwd: ... ]" wrapper removed. F5492: the raw subject was compared, so
// "Re: apple" sorted after "banana".
func baseSubject(subject string) string {
	s, _ := baseSubjectReply(subject)
	return s
}

// baseSubjectReply returns the base subject and whether extracting it
// removed a reply/forward marker ("re:", "fwd:", "(fwd)", "[fwd: ...]"),
// which the REFERENCES subject grouping needs (RFC 5256 §2.1, §3) (F5560).
func baseSubjectReply(subject string) (string, bool) {
	if dec, err := new(mime.WordDecoder).DecodeHeader(subject); err == nil {
		subject = dec
	}
	s := strings.ToLower(strings.Join(strings.Fields(subject), " "))
	reply := false
	for {
		before := s
		// (2) trailing "(fwd)" and whitespace
		for strings.HasSuffix(s, "(fwd)") {
			s = strings.TrimSpace(strings.TrimSuffix(s, "(fwd)"))
			reply = true
		}
		// (3)+(4) leading subj-refwd and subj-blob prefixes
		for {
			prev := s
			var refwd bool
			s, refwd = trimSubjectPrefix(s)
			reply = reply || refwd
			if s == prev {
				break
			}
		}
		// (6) "[fwd:" ... "]" wrapper
		if strings.HasPrefix(s, "[fwd:") && strings.HasSuffix(s, "]") {
			s = strings.TrimSpace(s[len("[fwd:") : len(s)-1])
			reply = true
		}
		if s == before {
			return s, reply
		}
	}
}

// trimSubjectPrefix removes one leading "re:", "fw:", "fwd:" (optionally
// with a "[blob]" before the colon), reporting true, or one "[blob]" that
// does not leave the subject empty.
func trimSubjectPrefix(s string) (string, bool) {
	for _, p := range []string{"re", "fwd", "fw"} {
		if !strings.HasPrefix(s, p) {
			continue
		}
		rest := strings.TrimLeft(s[len(p):], " ")
		if strings.HasPrefix(rest, "[") {
			if end := strings.IndexByte(rest, ']'); end > 0 && !strings.ContainsAny(rest[1:end], "[]") {
				rest = strings.TrimLeft(rest[end+1:], " ")
			}
		}
		if strings.HasPrefix(rest, ":") {
			return strings.TrimSpace(rest[1:]), true
		}
	}
	if strings.HasPrefix(s, "[") {
		if end := strings.IndexByte(s, ']'); end > 0 && !strings.ContainsAny(s[1:end], "[]") {
			if rest := strings.TrimSpace(s[end+1:]); rest != "" {
				return rest, false
			}
		}
	}
	return s, false
}

// sentDate is the RFC 5256 §2.2 sent date: the Date header, or the
// INTERNALDATE when the header is missing or cannot be parsed (F5561).
func sentDate(date string, internal time.Time) time.Time {
	if t, err := parseMessageDate(date); err == nil {
		return t
	}
	return internal
}

// addrMailbox returns the addr-mailbox (local part) of the first address
// in an address header, which SORT FROM / TO / CC compare (RFC 5256 §3).
func addrMailbox(header string) string {
	if strings.TrimSpace(header) == "" {
		return ""
	}
	addr := ""
	if list, err := mail.ParseAddressList(header); err == nil && len(list) > 0 {
		addr = list[0].Address
	} else {
		addr = header
		if l := strings.IndexByte(addr, '<'); l >= 0 {
			if r := strings.IndexByte(addr[l:], '>'); r > 0 {
				addr = addr[l+1 : l+r]
			}
		} else if c := strings.IndexByte(addr, ','); c >= 0 {
			addr = addr[:c]
		}
		addr = strings.TrimSpace(addr)
	}
	if at := strings.LastIndexByte(addr, '@'); at >= 0 {
		addr = addr[:at]
	}
	return addr
}

// ThreadAlgorithm represents the threading algorithm per RFC 5256
type ThreadAlgorithm string

const (
	ThreadReferences     ThreadAlgorithm = "REFERENCES"
	ThreadOrderedSubject ThreadAlgorithm = "ORDEREDSUBJECT"
)

// threadMessagesByReferences threads messages using REFERENCES algorithm
// Messages are linked by Message-ID headers per RFC 5256 Section 5
func threadMessagesByReferences(messages []*storage.MessageMetadata, seqNums []uint32) map[uint32][]uint32 {
	// Build a map of Message-ID -> sequence number
	idToSeq := make(map[string]uint32)
	children := make(map[uint32][]uint32)

	// Create seqNum to index mapping
	seqToIdx := make(map[uint32]int)
	for i, seq := range seqNums {
		seqToIdx[seq] = i
	}

	for i, msg := range messages {
		seq := seqNums[i]
		if msg.MessageID != "" {
			idToSeq[msg.MessageID] = seq
		}
	}

	// For each message, find its parent (In-Reply-To or References)
	for i, msg := range messages {
		seq := seqNums[i]
		added := false

		// Check In-Reply-To
		if msg.InReplyTo != "" {
			if parentSeq, ok := idToSeq[msg.InReplyTo]; ok {
				children[parentSeq] = append(children[parentSeq], seq)
				added = true
			}
		}

		// Check References header (may contain multiple IDs, use first that exists)
		if !added && len(msg.References) > 0 {
			for _, ref := range msg.References {
				if parentSeq, ok := idToSeq[ref]; ok {
					children[parentSeq] = append(children[parentSeq], seq)
					break
				}
			}
		}
	}

	return children
}

// threadMessagesByOrderedSubject threads messages by ORDEREDSUBJECT algorithm
// Messages with same subject are grouped together, ordered by date
func threadMessagesByOrderedSubject(messages []*storage.MessageMetadata, seqNums []uint32) map[uint32][]uint32 {
	// Group by normalized subject
	type msgInfo struct {
		seqNum uint32
		date   time.Time
	}
	subjectGroups := make(map[string][]msgInfo)

	for i, msg := range messages {
		normalizedSubject := strings.ToLower(strings.TrimSpace(msg.Subject))
		if normalizedSubject == "" {
			normalizedSubject = "(no subject)"
		}
		t, _ := parseMessageDate(msg.Date)
		subjectGroups[normalizedSubject] = append(subjectGroups[normalizedSubject], msgInfo{
			seqNum: seqNums[i],
			date:   t,
		})
	}

	// Sort each group by date
	for subject := range subjectGroups {
		sort.Slice(subjectGroups[subject], func(i, j int) bool {
			return subjectGroups[subject][i].date.Before(subjectGroups[subject][j].date)
		})
	}

	// Build thread tree - first message in each group is root
	children := make(map[uint32][]uint32)

	for _, group := range subjectGroups {
		if len(group) > 0 {
			root := group[0].seqNum
			for i := 1; i < len(group); i++ {
				children[root] = append(children[root], group[i].seqNum)
			}
		}
	}

	return children
}

// flattenThread returns all sequence numbers in a thread starting from root
func flattenThread(root uint32, children map[uint32][]uint32, visited map[uint32]bool) []uint32 {
	var result []uint32
	queue := []uint32{root}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		if visited[curr] {
			continue
		}
		visited[curr] = true
		result = append(result, curr)
		queue = append(queue, children[curr]...)
	}

	return result
}

// ThreadResult represents the result of a THREAD command
type ThreadResult struct {
	Threads [][]uint32
}

// threadMsg is one message given to the RFC 5256 threading algorithms.
type threadMsg struct {
	num     uint32   // number reported: sequence number or UID
	seq     uint32   // sequence number, the tie-break for equal dates
	msgID   string   // Message-ID without angle brackets ("" if none)
	refs    []string // References, or the first In-Reply-To id
	subject string
	date    time.Time // sent date (Date header, else INTERNALDATE)
}

// threadNode is a thread container; msg is nil for a dummy.
type threadNode struct {
	msg      *threadMsg
	parent   *threadNode
	children []*threadNode
}

func threadLink(parent, child *threadNode) {
	child.parent = parent
	parent.children = append(parent.children, child)
}

func threadUnlink(child *threadNode) {
	p := child.parent
	if p == nil {
		return
	}
	for i, c := range p.children {
		if c == child {
			p.children = append(p.children[:i:i], p.children[i+1:]...)
			break
		}
	}
	child.parent = nil
}

// threadReaches reports whether to is from or one of its descendants.
func threadReaches(from, to *threadNode) bool {
	for n := to; n != nil; n = n.parent {
		if n == from {
			return true
		}
	}
	return false
}

// threadMsgIDs extracts the "<id>" message ids of a header value.
func threadMsgIDs(v string) []string {
	var ids []string
	for {
		l := strings.IndexByte(v, '<')
		if l < 0 {
			return ids
		}
		r := strings.IndexByte(v[l+1:], '>')
		if r < 0 {
			return ids
		}
		if id := strings.TrimSpace(v[l+1 : l+1+r]); id != "" && !strings.ContainsAny(id, " \t<") {
			ids = append(ids, id)
		}
		v = v[l+1+r+1:]
	}
}

// threadIDs returns the message's own id and its references: the
// References ids or, without any, the first In-Reply-To id (RFC 5256 §3).
func threadIDs(messageID, inReplyTo, references string) (string, []string) {
	id := ""
	if ids := threadMsgIDs(messageID); len(ids) > 0 {
		id = ids[0]
	} else if v := strings.TrimSpace(messageID); v != "" && !strings.ContainsAny(v, " \t") {
		id = v
	}
	refs := threadMsgIDs(references)
	if len(refs) == 0 {
		if irt := threadMsgIDs(inReplyTo); len(irt) > 0 {
			refs = irt[:1]
		}
	}
	return id, refs
}

// readHeaderFields parses a header section (unfolding continuation lines);
// a malformed line ends it and the fields read so far are kept.
func readHeaderFields(hdr []byte) textproto.MIMEHeader {
	r := textproto.NewReader(bufio.NewReader(bytes.NewReader(append(append([]byte{}, hdr...), "\r\n\r\n"...))))
	h, _ := r.ReadMIMEHeader() // partial header on error, see above
	return h
}

// parseThreadHeader reads the (possibly folded) Message-ID, In-Reply-To,
// References, Subject and Date fields from a message header section.
func parseThreadHeader(hdr []byte) (id string, refs []string, subject, date string) {
	h := readHeaderFields(hdr)
	id, refs = threadIDs(h.Get("Message-Id"), h.Get("In-Reply-To"), strings.Join(h.Values("References"), " "))
	return id, refs, h.Get("Subject"), h.Get("Date")
}

// threadOrderedSubject implements ORDEREDSUBJECT (RFC 5256 §3): messages
// are grouped by base subject; the earliest is the parent, the rest are its
// children in sent-date order; threads are ordered by their first message.
func threadOrderedSubject(msgs []*threadMsg) []*threadNode {
	base := make(map[*threadMsg]string, len(msgs))
	sorted := append([]*threadMsg(nil), msgs...)
	for _, m := range sorted {
		base[m] = baseSubject(m.subject)
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if base[a] != base[b] {
			return base[a] < base[b]
		}
		return threadMsgBefore(a, b)
	})
	var roots []*threadNode
	var cur *threadNode
	for _, m := range sorted {
		n := &threadNode{msg: m}
		if cur != nil && base[cur.msg] == base[m] {
			threadLink(cur, n)
			continue
		}
		cur = n
		roots = append(roots, n)
	}
	sortThreadNodes(roots)
	return roots
}

// threadReferences implements the REFERENCES algorithm (RFC 5256 §3).
func threadReferences(msgs []*threadMsg) []*threadNode {
	table := map[string]*threadNode{}
	var all []*threadNode
	container := func(id string) *threadNode {
		if c, ok := table[id]; ok {
			return c
		}
		c := &threadNode{}
		table[id] = c
		all = append(all, c)
		return c
	}
	for i, m := range msgs {
		// (1)(B) the message's own container; a missing or duplicate
		// Message-ID gets a unique one.
		var c *threadNode
		if ex, ok := table[m.msgID]; m.msgID != "" && (!ok || ex.msg == nil) {
			c = container(m.msgID)
		} else {
			c = container(fmt.Sprintf("\x00%d", i))
		}
		c.msg = m
		// (1)(A) link the references parent-to-child where not yet linked
		// and no loop results.
		var prev *threadNode
		for _, ref := range m.refs {
			r := container(ref)
			if prev != nil && r.parent == nil && !threadReaches(r, prev) {
				threadLink(prev, r)
			}
			prev = r
		}
		// (1)(B) the last reference becomes the parent, replacing any
		// earlier link, unless that would make a loop.
		if prev != nil && !threadReaches(c, prev) && c.parent != prev {
			threadUnlink(c)
			threadLink(prev, c)
		}
	}
	// (2) root set, (4) prune dummies, (5) gather by subject, (6) sort.
	var roots []*threadNode
	for _, c := range all {
		if c.parent == nil {
			roots = append(roots, c)
		}
	}
	roots = threadGatherSubjects(pruneThreadNodes(roots, true))
	sortThreadNodes(roots)
	return roots
}

// pruneThreadNodes removes dummies without children and promotes the
// children of other dummies, except that a root-level dummy keeps two or
// more children (RFC 5256 §3 REFERENCES step 4).
func pruneThreadNodes(nodes []*threadNode, root bool) []*threadNode {
	var out []*threadNode
	for _, n := range nodes {
		n.children = pruneThreadNodes(n.children, false)
		if n.msg == nil {
			if len(n.children) == 0 {
				continue
			}
			if !root || len(n.children) == 1 {
				for _, ch := range n.children {
					ch.parent = n.parent
				}
				out = append(out, n.children...)
				continue
			}
		}
		out = append(out, n)
	}
	return out
}

// threadSubjectOf returns the base subject of a root (a dummy uses its
// first child) and whether it was a reply or forward.
func threadSubjectOf(n *threadNode) (string, bool) {
	if n.msg == nil {
		n = n.children[0]
	}
	return baseSubjectReply(n.msg.subject)
}

// threadGatherSubjects merges root threads with the same base subject
// (RFC 5256 §3 REFERENCES step 5).
func threadGatherSubjects(roots []*threadNode) []*threadNode {
	type entry struct {
		node  *threadNode
		reply bool
	}
	table := map[string]entry{}
	for _, r := range roots {
		s, reply := threadSubjectOf(r)
		if s == "" {
			continue
		}
		e, ok := table[s]
		if !ok || (r.msg == nil && e.node.msg != nil) ||
			(r.msg != nil && e.node.msg != nil && e.reply && !reply) {
			table[s] = entry{r, reply}
		}
	}
	merged := map[*threadNode]bool{}
	for _, r := range roots {
		s, reply := threadSubjectOf(r)
		e, ok := table[s]
		if s == "" || !ok || e.node == r {
			continue
		}
		t := e.node
		switch {
		case t.msg == nil && r.msg == nil:
			for _, ch := range r.children {
				threadLink(t, ch)
			}
			r.children = nil
		case t.msg == nil:
			threadLink(t, r)
		case r.msg != nil && reply && !e.reply:
			threadLink(t, r)
		default:
			d := &threadNode{}
			threadLink(d, t)
			threadLink(d, r)
			table[s] = entry{d, false}
		}
		merged[r] = true
	}
	var out []*threadNode
	seen := map[*threadNode]bool{}
	for _, r := range roots {
		if merged[r] {
			continue
		}
		for r.parent != nil {
			r = r.parent
		}
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	return out
}

// threadFirst returns the message that dates a container: its own, or a
// dummy's first child's once the children are sorted.
func threadFirst(n *threadNode) *threadMsg {
	for n.msg == nil {
		n = n.children[0]
	}
	return n.msg
}

func threadMsgBefore(a, b *threadMsg) bool {
	if !a.date.Equal(b.date) {
		return a.date.Before(b.date)
	}
	return a.seq < b.seq
}

// sortThreadNodes orders siblings by sent date, then sequence number
// (RFC 5256 §3 REFERENCES step 6), children first.
func sortThreadNodes(nodes []*threadNode) {
	for _, n := range nodes {
		sortThreadNodes(n.children)
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		return threadMsgBefore(threadFirst(nodes[i]), threadFirst(nodes[j]))
	})
}

// formatThreads renders the RFC 5256 §4 thread-list: "(1 2 (3)(4))(5)".
func formatThreads(roots []*threadNode) string {
	var b strings.Builder
	for _, r := range roots {
		b.WriteByte('(')
		writeThreadMembers(&b, r)
		b.WriteByte(')')
	}
	return b.String()
}

func writeThreadMembers(b *strings.Builder, n *threadNode) {
	if n.msg != nil {
		fmt.Fprintf(b, "%d", n.msg.num)
		switch len(n.children) {
		case 0:
			return
		case 1:
			b.WriteByte(' ')
			writeThreadMembers(b, n.children[0])
			return
		}
		b.WriteByte(' ')
	}
	for _, ch := range n.children {
		b.WriteByte('(')
		writeThreadMembers(b, ch)
		b.WriteByte(')')
	}
}
