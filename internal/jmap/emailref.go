package jmap

import (
	"encoding/base64"
	"strconv"
	"strings"
)

// F6645: an Email id is the content hash of its blob, so byte-identical
// messages share one id. RFC 8621 lets one Email live in several mailboxes
// (one copy per mailbox, the "primary" Email, id = blob hash), but a second
// copy of the same bytes in a mailbox that already holds one is a different
// message and must be addressable on its own. Such an extra copy gets the
// id "<hash>_<uid>_<base64url(mailbox)>" (RFC 8620 §1.2 id charset). The
// primary Email keeps the plain hash, so existing ids are unchanged.

// extraCopyID builds the id of an extra (same-mailbox duplicate) copy.
func extraCopyID(hash, mailbox string, uid uint32) string {
	return hash + "_" + strconv.FormatUint(uint64(uid), 10) + "_" +
		base64.RawURLEncoding.EncodeToString([]byte(mailbox))
}

// parseExtraCopyID splits an extra-copy id; ok is false for a plain hash.
func parseExtraCopyID(id string) (hash, mailbox string, uid uint32, ok bool) {
	parts := strings.SplitN(id, "_", 3)
	if len(parts) != 3 || parts[0] == "" {
		return "", "", 0, false
	}
	n, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return "", "", 0, false
	}
	name, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(name) == 0 {
		return "", "", 0, false
	}
	return parts[0], string(name), uint32(n), true
}

// allBlobCopies returns every stored copy (mailbox, uid) whose blob is hash,
// in mailbox-list then ascending-UID order.
func (s *Server) allBlobCopies(user, hash string) []emailCopy {
	var copies []emailCopy
	mailboxes, _ := s.db.ListMailboxes(user)
	for _, mbox := range mailboxes {
		uids, _ := s.db.GetMessageUIDs(user, mbox)
		for _, uid := range uids {
			meta, err := s.db.GetMessageMetadata(user, mbox, uid)
			if err != nil || meta == nil {
				continue
			}
			if meta.MessageID == hash {
				copies = append(copies, emailCopy{mailbox: mbox, uid: uid, meta: meta})
			}
		}
	}
	return copies
}

// extraCopy resolves an extra-copy id to its single stored copy.
func (s *Server) extraCopy(user, id string) []emailCopy {
	hash, mbox, uid, ok := parseExtraCopyID(id)
	if !ok {
		return nil
	}
	meta, err := s.db.GetMessageMetadata(user, mbox, uid)
	if err != nil || meta == nil || meta.MessageID != hash {
		return nil
	}
	return []emailCopy{{mailbox: mbox, uid: uid, meta: meta}}
}
