package imap

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

// BboltMailstore implements the Mailstore interface using bbolt database
type BboltMailstore struct {
	dataDir  string
	db       *storage.Database
	msgStore *storage.MessageStore
}

// NewBboltMailstore creates a new mailstore backed by bbolt
func NewBboltMailstore(dataDir string) (*BboltMailstore, error) {
	// Ensure data directory exists
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	dbPath := filepath.Join(dataDir, "mail.db")
	db, err := storage.OpenDatabase(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	msgStorePath := filepath.Join(dataDir, "messages")
	msgStore, err := storage.NewMessageStore(msgStorePath)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create message store: %w", err)
	}

	return &BboltMailstore{
		dataDir:  dataDir,
		db:       db,
		msgStore: msgStore,
	}, nil
}

// Close closes the mailstore
func (m *BboltMailstore) Close() error {
	if m.msgStore != nil {
		m.msgStore.Close()
	}
	if m.db != nil {
		return m.db.Close()
	}
	return nil
}

// Authenticate validates user credentials
func (m *BboltMailstore) Authenticate(username, password string) (bool, error) {
	return m.db.AuthenticateUser(username, password)
}

// SelectMailbox returns mailbox information
func (m *BboltMailstore) SelectMailbox(user, mailbox string) (*Mailbox, error) {
	// Get mailbox info from database
	mb, err := m.db.GetMailbox(user, mailbox)
	if err != nil {
		return nil, err
	}

	// Get message counts
	exists, recent, unseen, err := m.db.GetMailboxCounts(user, mailbox)
	if err != nil {
		return nil, err
	}

	return &Mailbox{
		Name:           mb.Name,
		Exists:         exists,
		Recent:         recent,
		Unseen:         unseen,
		UIDValidity:    mb.UIDValidity,
		UIDNext:        mb.UIDNext,
		Flags:          []string{"\\Answered", "\\Flagged", "\\Deleted", "\\Seen", "\\Draft"},
		PermanentFlags: []string{"\\Answered", "\\Flagged", "\\Deleted", "\\Seen", "\\Draft"},
	}, nil
}

// CreateMailbox creates a new mailbox
func (m *BboltMailstore) CreateMailbox(user, mailbox string) error {
	return m.db.CreateMailbox(user, mailbox)
}

// DeleteMailbox deletes a mailbox
func (m *BboltMailstore) DeleteMailbox(user, mailbox string) error {
	return m.db.DeleteMailbox(user, mailbox)
}

// RenameMailbox renames a mailbox
func (m *BboltMailstore) RenameMailbox(user, oldName, newName string) error {
	return m.db.RenameMailbox(user, oldName, newName)
}

// ListMailboxes lists mailboxes matching a pattern
func (m *BboltMailstore) ListMailboxes(user, pattern string) ([]string, error) {
	mailboxes, err := m.db.ListMailboxes(user)
	if err != nil {
		return nil, err
	}

	// Convert IMAP pattern to regex-like matching
	// * matches anything
	// % matches anything except hierarchy delimiter
	var result []string
	for _, mb := range mailboxes {
		if matchPattern(mb, pattern) {
			result = append(result, mb)
		}
	}

	sort.Strings(result)
	return result, nil
}

// matchPattern checks if a mailbox name matches an IMAP pattern
func matchPattern(name, pattern string) bool {
	// Simple pattern matching
	if pattern == "*" {
		return true
	}

	// Convert pattern to simple wildcard matching
	// Replace * with .* and escape other special chars
	// For now, implement basic matching
	if strings.HasSuffix(pattern, "*") {
		prefix := pattern[:len(pattern)-1]
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}

	if strings.HasPrefix(pattern, "*") {
		suffix := pattern[1:]
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}

	return name == pattern
}

// FetchMessages retrieves messages by sequence set
func (m *BboltMailstore) FetchMessages(user, mailbox string, seqSet string, items []string) ([]*Message, error) {
	// Parse sequence set
	ranges, err := ParseSequenceSet(seqSet)
	if err != nil {
		return nil, err
	}

	// Get message UIDs for the mailbox
	uids, err := m.db.GetMessageUIDs(user, mailbox)
	if err != nil {
		return nil, err
	}

	var messages []*Message
	for i, uid := range uids {
		// IMAP uses 1-based sequence numbers
		seqNum := uint32(i + 1)

		// Check if this sequence number is in the requested set
		inSet := false
		for _, r := range ranges {
			if r.Contains(seqNum, uint32(len(uids))) {
				inSet = true
				break
			}
		}

		if !inSet {
			continue
		}

		// Fetch message
		msg, err := m.getMessage(user, mailbox, seqNum, uid, items)
		if err != nil {
			continue
		}

		messages = append(messages, msg)
	}

	return messages, nil
}

// getMessage retrieves a single message
func (m *BboltMailstore) getMessage(user, mailbox string, seqNum, uid uint32, items []string) (*Message, error) {
	// Get message metadata from database
	meta, err := m.db.GetMessageMetadata(user, mailbox, uid)
	if err != nil {
		return nil, err
	}

	msg := &Message{
		SeqNum:       seqNum,
		UID:          uid,
		Flags:        meta.Flags,
		InternalDate: meta.InternalDate,
		Size:         meta.Size,
		Subject:      meta.Subject,
		Date:         meta.Date,
		From:         meta.From,
		To:           meta.To,
	}

	// Load message data if needed
	needsData := false
	for _, item := range items {
		item = strings.ToUpper(item)
		if item == "RFC822" || item == "BODY" || strings.HasPrefix(item, "BODY[") {
			needsData = true
			break
		}
	}

	if needsData {
		data, err := m.msgStore.ReadMessage(user, meta.MessageID)
		if err == nil {
			msg.Data = data
		}
	}

	return msg, nil
}

// StoreFlags updates message flags
func (m *BboltMailstore) StoreFlags(user, mailbox string, seqSet string, flags []string, add bool) error {
	// Parse sequence set
	ranges, err := ParseSequenceSet(seqSet)
	if err != nil {
		return err
	}

	// Get message UIDs
	uids, err := m.db.GetMessageUIDs(user, mailbox)
	if err != nil {
		return err
	}

	for i, uid := range uids {
		// IMAP uses 1-based sequence numbers
		seqNum := uint32(i + 1)

		// Check if in set
		inSet := false
		for _, r := range ranges {
			if r.Contains(seqNum, uint32(len(uids))) {
				inSet = true
				break
			}
		}

		if !inSet {
			continue
		}

		// Get current metadata
		meta, err := m.db.GetMessageMetadata(user, mailbox, uid)
		if err != nil {
			continue
		}

		// Update flags
		if add {
			// Add flags
			for _, flag := range flags {
				if !hasFlag(meta.Flags, flag) {
					meta.Flags = append(meta.Flags, flag)
				}
			}
		} else {
			// Remove flags
			var newFlags []string
			for _, f := range meta.Flags {
				if !hasFlag(flags, f) {
					newFlags = append(newFlags, f)
				}
			}
			meta.Flags = newFlags
		}

		// Save updated metadata
		m.db.UpdateMessageMetadata(user, mailbox, uid, meta)

		// Notify about flag changes
		GetNotificationHub().NotifyFlagsChanged(user, mailbox, uid, seqNum, meta.Flags)
	}

	return nil
}

// hasFlag checks if a flag is in the list
func hasFlag(flags []string, flag string) bool {
	flag = strings.ToUpper(flag)
	for _, f := range flags {
		if strings.ToUpper(f) == flag {
			return true
		}
	}
	return false
}

// Expunge removes messages marked as deleted
func (m *BboltMailstore) Expunge(user, mailbox string) error {
	// Get all messages
	uids, err := m.db.GetMessageUIDs(user, mailbox)
	if err != nil {
		return err
	}

	for _, uid := range uids {
		meta, err := m.db.GetMessageMetadata(user, mailbox, uid)
		if err != nil {
			continue
		}

		// Check if deleted
		if hasFlag(meta.Flags, "\\Deleted") {
			// Delete message
			m.msgStore.DeleteMessage(user, meta.MessageID)
			m.db.DeleteMessage(user, mailbox, uid)
		}
	}

	return nil
}

// AppendMessage appends a message to a mailbox
func (m *BboltMailstore) AppendMessage(user, mailbox string, flags []string, date time.Time, data []byte) error {
	// Store message
	messageID, err := m.msgStore.StoreMessage(user, data)
	if err != nil {
		return err
	}

	// Get next UID
	uid, err := m.db.GetNextUID(user, mailbox)
	if err != nil {
		return err
	}

	// Parse basic headers for indexing
	subject, from, to, dateStr := parseMessageHeaders(data)

	// Store metadata
	meta := &storage.MessageMetadata{
		MessageID:    messageID,
		UID:          uid,
		Flags:        flags,
		InternalDate: date,
		Size:         int64(len(data)),
		Subject:      subject,
		Date:         dateStr,
		From:         from,
		To:           to,
	}

	if err := m.db.StoreMessageMetadata(user, mailbox, uid, meta); err != nil {
		return err
	}

	// Get the sequence number for the new message
	uids, err := m.db.GetMessageUIDs(user, mailbox)
	if err == nil {
		seqNum := uint32(len(uids))
		// Notify subscribers about the new message
		GetNotificationHub().NotifyNewMessage(user, mailbox, uid, seqNum)
	}

	return nil
}

// parseMessageHeaders extracts basic headers from message data
func parseMessageHeaders(data []byte) (subject, from, to, date string) {
	// Simple header parsing
	headers := string(data)
	if idx := strings.Index(headers, "\r\n\r\n"); idx != -1 {
		headers = headers[:idx]
	} else if idx := strings.Index(headers, "\n\n"); idx != -1 {
		headers = headers[:idx]
	}

	for _, line := range strings.Split(headers, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(strings.ToLower(line), "subject:") {
			subject = strings.TrimSpace(line[8:])
		} else if strings.HasPrefix(strings.ToLower(line), "from:") {
			from = strings.TrimSpace(line[5:])
		} else if strings.HasPrefix(strings.ToLower(line), "to:") {
			to = strings.TrimSpace(line[3:])
		} else if strings.HasPrefix(strings.ToLower(line), "date:") {
			date = strings.TrimSpace(line[5:])
		}
	}

	return subject, from, to, date
}

// SearchMessages searches for messages matching criteria
func (m *BboltMailstore) SearchMessages(user, mailbox string, criteria SearchCriteria) ([]uint32, error) {
	// Get all message UIDs
	uids, err := m.db.GetMessageUIDs(user, mailbox)
	if err != nil {
		return nil, err
	}

	var results []uint32
	seqNum := uint32(0)

	for _, uid := range uids {
		seqNum++

		meta, err := m.db.GetMessageMetadata(user, mailbox, uid)
		if err != nil {
			continue
		}

		// Check criteria
		if matchesCriteria(meta, &criteria) {
			results = append(results, seqNum)
		}
	}

	return results, nil
}

// matchesCriteria checks if a message matches search criteria
func matchesCriteria(meta *storage.MessageMetadata, criteria *SearchCriteria) bool {
	// Handle NOT
	if criteria.Not != nil {
		if matchesCriteria(meta, criteria.Not) {
			return false
		}
	}

	// Handle OR
	if criteria.Or[0] != nil && criteria.Or[1] != nil {
		if !matchesCriteria(meta, criteria.Or[0]) && !matchesCriteria(meta, criteria.Or[1]) {
			return false
		}
	}

	// Check flag criteria
	if criteria.All {
		return true
	}
	if criteria.Answered && !hasFlag(meta.Flags, "\\Answered") {
		return false
	}
	if criteria.Deleted && !hasFlag(meta.Flags, "\\Deleted") {
		return false
	}
	if criteria.Flagged && !hasFlag(meta.Flags, "\\Flagged") {
		return false
	}
	if criteria.Seen && !hasFlag(meta.Flags, "\\Seen") {
		return false
	}
	if criteria.Unanswered && hasFlag(meta.Flags, "\\Answered") {
		return false
	}
	if criteria.Undeleted && hasFlag(meta.Flags, "\\Deleted") {
		return false
	}
	if criteria.Unflagged && hasFlag(meta.Flags, "\\Flagged") {
		return false
	}
	if criteria.Unseen && hasFlag(meta.Flags, "\\Seen") {
		return false
	}
	if criteria.New && !hasFlag(meta.Flags, "\\Recent") {
		return false
	}
	if criteria.Old && hasFlag(meta.Flags, "\\Recent") {
		return false
	}
	if criteria.Recent && !hasFlag(meta.Flags, "\\Recent") {
		return false
	}
	if criteria.Draft && !hasFlag(meta.Flags, "\\Draft") {
		return false
	}
	if criteria.Undraft && hasFlag(meta.Flags, "\\Draft") {
		return false
	}

	// Check string criteria
	if criteria.From != "" && !strings.Contains(strings.ToLower(meta.From), strings.ToLower(criteria.From)) {
		return false
	}
	if criteria.To != "" && !strings.Contains(strings.ToLower(meta.To), strings.ToLower(criteria.To)) {
		return false
	}
	if criteria.Subject != "" && !strings.Contains(strings.ToLower(meta.Subject), strings.ToLower(criteria.Subject)) {
		return false
	}

	// Check size criteria
	if criteria.Larger > 0 && meta.Size <= criteria.Larger {
		return false
	}
	if criteria.Smaller > 0 && meta.Size >= criteria.Smaller {
		return false
	}

	return true
}

// CopyMessages copies messages to another mailbox
func (m *BboltMailstore) CopyMessages(user, sourceMailbox, destMailbox string, seqSet string) error {
	// Parse sequence set
	ranges, err := ParseSequenceSet(seqSet)
	if err != nil {
		return err
	}

	// Get source message UIDs
	uids, err := m.db.GetMessageUIDs(user, sourceMailbox)
	if err != nil {
		return err
	}

	for i, uid := range uids {
		seqNum := uint32(i + 1) // IMAP uses 1-based sequence numbers
		// Check if in set
		inSet := false
		for _, r := range ranges {
			if r.Contains(seqNum, uint32(len(uids))) {
				inSet = true
				break
			}
		}

		if !inSet {
			continue
		}

		// Get source metadata
		meta, err := m.db.GetMessageMetadata(user, sourceMailbox, uid)
		if err != nil {
			continue
		}

		// Get message data
		data, err := m.msgStore.ReadMessage(user, meta.MessageID)
		if err != nil {
			continue
		}

		// Get next UID for destination
		newUID, err := m.db.GetNextUID(user, destMailbox)
		if err != nil {
			continue
		}

		// Copy message
		newMessageID, err := m.msgStore.StoreMessage(user, data)
		if err != nil {
			continue
		}

		// Store metadata in destination
		newMeta := &storage.MessageMetadata{
			MessageID:    newMessageID,
			UID:          newUID,
			Flags:        meta.Flags,
			InternalDate: meta.InternalDate,
			Size:         meta.Size,
			Subject:      meta.Subject,
			Date:         meta.Date,
			From:         meta.From,
			To:           meta.To,
		}

		m.db.StoreMessageMetadata(user, destMailbox, newUID, newMeta)
	}

	return nil
}

// MoveMessages moves messages to another mailbox
func (m *BboltMailstore) MoveMessages(user, sourceMailbox, destMailbox string, seqSet string) error {
	// First copy
	if err := m.CopyMessages(user, sourceMailbox, destMailbox, seqSet); err != nil {
		return err
	}

	// Then mark as deleted in source
	ranges, err := ParseSequenceSet(seqSet)
	if err != nil {
		return err
	}

	uids, err := m.db.GetMessageUIDs(user, sourceMailbox)
	if err != nil {
		return err
	}

	for i, uid := range uids {
		seqNum := uint32(i + 1) // IMAP uses 1-based sequence numbers
		inSet := false
		for _, r := range ranges {
			if r.Contains(seqNum, uint32(len(uids))) {
				inSet = true
				break
			}
		}

		if !inSet {
			continue
		}

		meta, err := m.db.GetMessageMetadata(user, sourceMailbox, uid)
		if err != nil {
			continue
		}

		// Add deleted flag
		if !hasFlag(meta.Flags, "\\Deleted") {
			meta.Flags = append(meta.Flags, "\\Deleted")
			m.db.UpdateMessageMetadata(user, sourceMailbox, uid, meta)
		}
	}

	return nil
}

// GetNextUID returns the next UID for a mailbox
func (m *BboltMailstore) GetNextUID(user, mailbox string) (uint32, error) {
	return m.db.GetNextUID(user, mailbox)
}

// UpdateMessageMetadata updates message metadata
func (m *BboltMailstore) UpdateMessageMetadata(user, mailbox string, uid uint32, meta *storage.MessageMetadata) error {
	return m.db.UpdateMessageMetadata(user, mailbox, uid, meta)
}

// Helper types for storage package integration

// parseSeqSet parses a sequence set string into individual sequence numbers
func parseSeqSet(set string, maxSeq uint32) ([]uint32, error) {
	var seqs []uint32

	parts := strings.Split(set, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)

		if strings.Contains(part, ":") {
			rangeParts := strings.Split(part, ":")
			if len(rangeParts) != 2 {
				return nil, fmt.Errorf("invalid range: %s", part)
			}

			start, err := parseSeqNum(rangeParts[0], maxSeq)
			if err != nil {
				return nil, err
			}

			end, err := parseSeqNum(rangeParts[1], maxSeq)
			if err != nil {
				return nil, err
			}

			for i := start; i <= end; i++ {
				seqs = append(seqs, i)
			}
		} else {
			num, err := parseSeqNum(part, maxSeq)
			if err != nil {
				return nil, err
			}
			seqs = append(seqs, num)
		}
	}

	return seqs, nil
}

func parseSeqNum(s string, maxSeq uint32) (uint32, error) {
	if s == "*" {
		return maxSeq, nil
	}

	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid sequence number: %s", s)
	}

	return uint32(n), nil
}

// EnsureDefaultMailboxes creates default mailboxes (INBOX, Sent, Drafts, Junk, Trash, Archive)
// for the given user if they do not already exist. Errors creating individual mailboxes
// are silently ignored since the mailbox creation is idempotent.
func (m *BboltMailstore) EnsureDefaultMailboxes(user string) error {
	defaults := []string{"INBOX", "Sent", "Drafts", "Junk", "Trash", "Archive"}
	for _, name := range defaults {
		_ = m.CreateMailbox(user, name)
	}
	return nil
}
