package main

import (
	"time"

	"github.com/umailserver/umailserver/internal/imap"
	"github.com/umailserver/umailserver/internal/storage"
)

// migrateMailboxIndex files migrated mail through the IMAP APPEND path, so it
// lands in the same mailbox index (data_dir/mail/mail.db) the servers read
// (F7788).
type migrateMailboxIndex struct {
	store *imap.BboltMailstore
	db    *storage.Database
	// held caches, per user and folder, the message ids already filed; built
	// on first use so a large folder is scanned once, not once per message.
	held map[string]map[string]bool
}

func newMigrateMailboxIndex(db *storage.Database, msgStore *storage.MessageStore) *migrateMailboxIndex {
	return &migrateMailboxIndex{
		store: imap.NewBboltMailstoreWithInterfaces(db, msgStore),
		db:    db,
		held:  make(map[string]map[string]bool),
	}
}

func (m *migrateMailboxIndex) CreateMailbox(user, mailbox string) error {
	return m.store.CreateMailbox(user, mailbox)
}

func (m *migrateMailboxIndex) AppendMessage(user, mailbox string, flags []string, date time.Time, data []byte) error {
	return m.store.AppendMessage(user, mailbox, flags, date, data)
}

func (m *migrateMailboxIndex) HasMessage(user, mailbox, messageID string) bool {
	key := user + "\x00" + mailbox
	set, ok := m.held[key]
	if !ok {
		set = make(map[string]bool)
		if uids, err := m.db.GetMessageUIDs(user, mailbox); err == nil {
			for _, uid := range uids {
				if meta, err := m.db.GetMessageMetadata(user, mailbox, uid); err == nil {
					set[meta.MessageID] = true
				}
			}
		}
		m.held[key] = set
	}
	if set[messageID] {
		return true
	}
	set[messageID] = true // about to be filed by the caller
	return false
}
