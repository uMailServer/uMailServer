// Package storage provides data storage for the mail server
package storage

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.etcd.io/bbolt"
)

// ---------------------------------------------------------------------------
// ACL (Access Control List) — RFC 4314
// ---------------------------------------------------------------------------

// ACLRights represents rights granted to a user on a mailbox (RFC 4314)
type ACLRights uint8

const (
	ACLLookup    ACLRights = 1 << 0 // User may see the mailbox in LIST (RFC 4314 Section 3)
	ACLRead      ACLRights = 1 << 1 // User may read messages (SELECT EXAMINE)
	ACLSeen      ACLRights = 1 << 2 // User may set/clear \Seen flag
	ACLWrite     ACLRights = 1 << 3 // User may write messages (APPEND, COPY into)
	ACLWriteSeen ACLRights = 1 << 4 // User may set/clear flags other than \Seen and \Deleted
	ACLDelete    ACLRights = 1 << 5 // User may set/clear \Deleted flag
	ACLExpunge   ACLRights = 1 << 6 // User may permanently remove messages (EXPUNGE)
	ACLCreate    ACLRights = 1 << 7 // User may CREATE new sub-mailboxes or RENAME
)

// ACLAll grants all rights
const ACLAll ACLRights = ACLLookup | ACLRead | ACLSeen | ACLWrite | ACLWriteSeen | ACLDelete | ACLExpunge | ACLCreate

// ACLEntry represents a single ACL grant
type ACLEntry struct {
	Grantee   string    `json:"grantee"`    // user granted access
	Rights    ACLRights `json:"rights"`     // bitmask of rights
	GrantedAt time.Time `json:"granted_at"` // when granted
	GrantedBy string    `json:"granted_by"` // who granted this access
}

// String renders the RFC 4314 rights vocabulary for the modeled bits:
// l (lookup), r (read), s (seen), w (write: flags other than \Seen/\Deleted),
// i (insert: APPEND/COPY into), t (delete-messages: \Deleted), e (expunge),
// k (create-mailboxes). Rights the model does not represent (p post, x
// delete-mailbox, a admin) render as nothing.
func (r ACLRights) String() string {
	var b strings.Builder
	if r&ACLLookup != 0 {
		b.WriteByte('l')
	}
	if r&ACLRead != 0 {
		b.WriteByte('r')
	}
	if r&ACLSeen != 0 {
		b.WriteByte('s')
	}
	if r&ACLWriteSeen != 0 {
		b.WriteByte('w')
	}
	if r&ACLWrite != 0 {
		b.WriteByte('i')
	}
	if r&ACLDelete != 0 {
		b.WriteByte('t')
	}
	if r&ACLExpunge != 0 {
		b.WriteByte('e')
	}
	if r&ACLCreate != 0 {
		b.WriteByte('k')
	}
	return b.String()
}

// aclKey builds the bbolt key for an ACL entry
// Key format: "acl:{owner}:{mailbox}:{grantee}"
func aclKey(owner, mailbox, grantee string) string {
	return fmt.Sprintf("acl:%s:%s:%s", owner, mailbox, grantee)
}

// aclOwnerMailboxPrefix returns the prefix for scanning all ACL entries for a mailbox
func aclOwnerMailboxPrefix(owner, mailbox string) string {
	return fmt.Sprintf("acl:%s:%s:", owner, mailbox)
}

// aclKeyBelongsTo reports whether the ACL bucket entry (k, v) belongs to
// exactly owner/mailbox. Keys are "acl:owner:mailbox:grantee" and mailbox
// names may contain colons, so a prefix scan for mailbox "Work" also matches
// the entries of "Work:Old". The grantee stored in the value disambiguates
// (F6121).
func aclKeyBelongsTo(owner, mailbox string, k, v []byte) bool {
	var entry ACLEntry
	if err := json.Unmarshal(v, &entry); err == nil {
		return string(k) == aclKey(owner, mailbox, entry.Grantee)
	}
	prefix := aclOwnerMailboxPrefix(owner, mailbox)
	return len(k) > len(prefix) && !strings.Contains(string(k[len(prefix):]), ":")
}

// ParseACLRights parses an RFC 4314 rights string (e.g. "lrs", "-e") into an
// ACLRights bitmask, and reports whether a leading '-' marked the rights for
// removal.
//
// The accepted letters follow RFC 4314 §2.2.1 as mapped onto the modeled
// bits: l lookup, r read, s seen, w write (flags other than \Seen/\Deleted),
// i insert (APPEND/COPY into), t delete-messages (\Deleted), e expunge,
// k create-mailboxes. Letters for rights this model does not represent
// (p post, x delete-mailbox, a admin, and the deprecated c/d) are rejected
// rather than silently mis-mapped.
//
// RFC 4314 section 3.1: a '-' prefix means the listed rights are REMOVED from
// the grantee's EXISTING set. This function cannot compute that on its own --
// it does not know what the grantee already holds -- so it returns the parsed
// mask together with the negative flag and the caller must apply it against the
// current rights with &^. Complementing the mask is not equivalent: it yields
// every right the caller did NOT list, turning a revocation into a grant.
func ParseACLRights(s string) (ACLRights, bool, error) {
	if s == "" {
		return 0, false, nil
	}

	// Negative indicator removes rights
	negative := false
	if s[0] == '-' {
		negative = true
		s = s[1:]
	}

	var rights ACLRights
	for _, c := range s {
		switch c {
		case 'l':
			rights |= ACLLookup
		case 'r':
			rights |= ACLRead
		case 's':
			rights |= ACLSeen
		case 'w':
			rights |= ACLWriteSeen
		case 'i':
			rights |= ACLWrite
		case 't':
			rights |= ACLDelete
		case 'e':
			rights |= ACLExpunge
		case 'k':
			rights |= ACLCreate
		default:
			return 0, false, fmt.Errorf("invalid right character: %c", c)
		}
	}

	return rights, negative, nil
}

// GetACL retrieves the rights a grantee has on a specific mailbox.
// Returns ACLRights(0) and nil error if no ACL is set.
func (db *Database) GetACL(owner, mailbox, grantee string) (ACLRights, error) {
	if db.bolt == nil {
		return 0, nil
	}

	var rights ACLRights
	err := db.bolt.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("acl"))
		if b == nil {
			return nil
		}
		data := b.Get([]byte(aclKey(owner, mailbox, grantee)))
		if data == nil {
			return nil
		}
		var entry ACLEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			return err
		}
		rights = entry.Rights
		return nil
	})
	return rights, err
}

// SetACL sets or updates an ACL entry. Pass rights=0 to remove the entry.
// grantingUser is recorded as GrantedBy.
func (db *Database) SetACL(owner, mailbox, grantee string, rights ACLRights, grantingUser string) error {
	if db.bolt == nil {
		return nil
	}

	return db.bolt.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte("acl"))
		if err != nil {
			return err
		}

		if rights == 0 {
			return b.Delete([]byte(aclKey(owner, mailbox, grantee)))
		}

		entry := ACLEntry{
			Grantee:   grantee,
			Rights:    rights,
			GrantedAt: time.Now(),
			GrantedBy: grantingUser,
		}
		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		return b.Put([]byte(aclKey(owner, mailbox, grantee)), data)
	})
}

// DeleteACL removes all ACL entries for a mailbox if grantee is empty,
// or a single entry if grantee is specified.
func (db *Database) DeleteACL(owner, mailbox, grantee string) error {
	if db.bolt == nil {
		return nil
	}

	return db.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("acl"))
		if b == nil {
			return nil
		}

		if grantee != "" {
			return b.Delete([]byte(aclKey(owner, mailbox, grantee)))
		}

		prefix := aclOwnerMailboxPrefix(owner, mailbox)
		var doomed [][]byte
		c := b.Cursor()
		for k, v := c.Seek([]byte(prefix)); k != nil && strings.HasPrefix(string(k), prefix); k, v = c.Next() {
			if aclKeyBelongsTo(owner, mailbox, k, v) {
				doomed = append(doomed, append([]byte(nil), k...))
			}
		}
		for _, k := range doomed {
			if err := b.Delete(k); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListACL returns all ACL entries for a mailbox.
func (db *Database) ListACL(owner, mailbox string) ([]ACLEntry, error) {
	if db.bolt == nil {
		return nil, nil
	}

	var entries []ACLEntry
	err := db.bolt.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("acl"))
		if b == nil {
			return nil
		}

		prefix := aclOwnerMailboxPrefix(owner, mailbox)
		c := b.Cursor()
		for k, v := c.Seek([]byte(prefix)); k != nil && strings.HasPrefix(string(k), prefix); k, v = c.Next() {
			var entry ACLEntry
			if err := json.Unmarshal(v, &entry); err != nil {
				return err
			}
			if !aclKeyBelongsTo(owner, mailbox, k, v) {
				continue
			}
			entries = append(entries, entry)
		}
		return nil
	})
	return entries, err
}

// CanAccess checks whether user has at least the required rights on a mailbox.
// If user is the owner, all rights are granted. Otherwise, ACL is consulted.
func (db *Database) CanAccess(user, owner, mailbox string, required ACLRights) (bool, error) {
	if user == owner {
		return true, nil
	}

	rights, err := db.GetACL(owner, mailbox, user)
	if err != nil {
		return false, err
	}
	return rights&required == required, nil
}

// ListMailboxesSharedWith returns all mailboxes shared with a given user (where user is grantee).
// Returns list in format "owner:mailbox".
func (db *Database) ListMailboxesSharedWith(user string) ([]string, error) {
	if db.bolt == nil {
		return nil, nil
	}

	var result []string
	err := db.bolt.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("acl"))
		if b == nil {
			return nil
		}
		c := b.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			key := string(k)
			if !strings.HasPrefix(key, "acl:") {
				continue
			}
			ownerMailbox, grantee, ok := strings.Cut(key[len("acl:"):], ":")
			if !ok {
				continue
			}
			// Mailbox names may contain colons; the grantee is the final field.
			last := strings.LastIndexByte(grantee, ':')
			if last >= 0 && grantee[last+1:] == user {
				result = append(result, ownerMailbox+":"+grantee[:last])
			}
		}
		return nil
	})
	return result, err
}

// ListGranteesMailboxes returns all mailboxes owned by owner that are shared with others.
func (db *Database) ListGranteesMailboxes(owner string) ([]string, error) {
	if db.bolt == nil {
		return nil, nil
	}

	var result []string
	prefix := fmt.Sprintf("acl:%s:", owner)
	err := db.bolt.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("acl"))
		if b == nil {
			return nil
		}
		seen := make(map[string]bool)
		c := b.Cursor()
		for k, _ := c.Seek([]byte(prefix)); k != nil && strings.HasPrefix(string(k), prefix); k, _ = c.Next() {
			mailboxGrantee := string(k[len(prefix):])
			if last := strings.LastIndexByte(mailboxGrantee, ':'); last >= 0 {
				mailbox := mailboxGrantee[:last]
				if !seen[mailbox] {
					seen[mailbox] = true
					result = append(result, mailbox)
				}
			}
		}
		return nil
	})
	return result, err
}
