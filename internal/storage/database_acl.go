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

// String returns human-readable rights (e.g., "lrswipkxtecda")
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
	if r&ACLWrite != 0 {
		b.WriteByte('w')
	}
	if r&ACLWriteSeen != 0 {
		b.WriteByte('i')
	}
	if r&ACLDelete != 0 {
		b.WriteByte('p')
	}
	if r&ACLExpunge != 0 {
		b.WriteByte('x')
	}
	if r&ACLCreate != 0 {
		b.WriteByte('c')
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

// ParseACLRights parses rights string (e.g., "lrswipkxtecda" or "-lrswipkxtecda" or empty) into ACLRights bitmask
func ParseACLRights(s string) (ACLRights, error) {
	if s == "" {
		return 0, nil
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
			rights |= ACLWrite
		case 'i':
			rights |= ACLWriteSeen
		case 'p':
			rights |= ACLDelete
		case 'k':
			rights |= ACLExpunge
		case 'x':
			rights |= ACLCreate
		default:
			return 0, fmt.Errorf("invalid right character: %c", c)
		}
	}

	if negative {
		return ^rights, nil
	}
	return rights, nil
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
		c := b.Cursor()
		for k, _ := c.Seek([]byte(prefix)); k != nil && strings.HasPrefix(string(k), prefix); k, _ = c.Next() {
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
			parts := strings.SplitN(key, ":", 4)
			if len(parts) == 4 && parts[3] == user {
				result = append(result, fmt.Sprintf("%s:%s", parts[1], parts[2]))
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
			parts := strings.SplitN(string(k), ":", 4)
			if len(parts) == 4 {
				mailbox := parts[2]
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
