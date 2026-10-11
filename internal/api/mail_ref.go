package api

import (
	"encoding/base64"
	"strconv"
	"strings"
)

// F6645: the REST message id is the blob content hash, so byte-identical
// messages (two copies in one folder, or the same mail in INBOX and Sent)
// share it, and a delete/flag/move by that id used to hit every copy. When a
// hash is held by more than one message of the account the list reports each
// copy under "<hash>_<uid>_<base64url(mailbox)>" instead; that id addresses
// exactly one (folder, uid) entry. A hash held once keeps its plain id, so
// ids the webmail already stores stay valid.

func qualifiedMailID(hash, mailbox string, uid uint32) string {
	return hash + "_" + strconv.FormatUint(uint64(uid), 10) + "_" +
		base64.RawURLEncoding.EncodeToString([]byte(mailbox))
}

// parseQualifiedMailID splits a copy-qualified id; ok is false for a plain
// content-hash id.
func parseQualifiedMailID(id string) (hash, mailbox string, uid uint32, ok bool) {
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

// duplicatedHashes reports which of the wanted blob hashes are held by more
// than one message entry anywhere in the user's mailboxes.
func (h *MailHandler) duplicatedHashes(user string, want map[string]bool) map[string]bool {
	dups := map[string]bool{}
	if h.mailDB == nil || len(want) == 0 {
		return dups
	}
	seen := make(map[string]int, len(want))
	boxes, err := h.mailDB.ListMailboxes(user)
	if err != nil {
		return dups
	}
	for _, mb := range boxes {
		uids, err := h.mailDB.GetMessageUIDs(user, mb)
		if err != nil {
			continue
		}
		for _, uid := range uids {
			meta, err := h.mailDB.GetMessageMetadata(user, mb, uid)
			if err != nil || meta == nil || !want[meta.MessageID] {
				continue
			}
			seen[meta.MessageID]++
			if seen[meta.MessageID] > 1 {
				dups[meta.MessageID] = true
			}
		}
	}
	return dups
}

// blobStillReferenced reports whether any message entry of the user still
// uses the blob.
func (h *MailHandler) blobStillReferenced(user, hash string) bool {
	return h.countHash(user, hash) > 0
}

func (h *MailHandler) countHash(user, hash string) int {
	n := 0
	boxes, _ := h.mailDB.ListMailboxes(user)
	for _, mb := range boxes {
		uids, _ := h.mailDB.GetMessageUIDs(user, mb)
		for _, uid := range uids {
			if meta, err := h.mailDB.GetMessageMetadata(user, mb, uid); err == nil && meta != nil && meta.MessageID == hash {
				n++
			}
		}
	}
	return n
}

func splitQualified(id string) (string, uint32, bool) {
	hash, _, uid, ok := parseQualifiedMailID(id)
	return hash, uid, ok
}
