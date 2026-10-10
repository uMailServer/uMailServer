package imap

import (
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/umailserver/umailserver/internal/storage"
)

// BboltMailstore implements the Mailstore interface using bbolt database
type BboltMailstore struct {
	dataDir  string
	db       *storage.Database
	msgStore *storage.MessageStore

	// MDN tracking
	mdnSent    map[string]bool // Message-Id -> true if MDN sent
	mdnSentMu  sync.Mutex
	mdnHandler func(from, to, messageID, inReplyTo string, msg []byte) error
	mdnSem     chan struct{} // Bounds concurrent MDN goroutines

	// quotaLimit returns the byte quota of a user (<= 0 = unlimited). Nil
	// means no quota is enforced (F5730).
	quotaLimit func(user string) int64
}

// SetQuotaLimitFunc installs the per-user quota lookup used by APPEND, COPY
// and MOVE. Usage is recounted from the blob store, so EXPUNGE (which
// releases blobs) frees quota without separate bookkeeping. The orchestrator
// wires this from the accounts database (QuotaLimit of the account).
func (m *BboltMailstore) SetQuotaLimitFunc(f func(user string) int64) {
	m.quotaLimit = f
}

// storeBlob stores data for user, enforcing the account quota when a quota
// lookup is installed. Over-quota returns an error matching
// storage.ErrQuotaExceeded.
func (m *BboltMailstore) storeBlob(user string, data []byte) (string, error) {
	if m.quotaLimit != nil {
		if limit := m.quotaLimit(user); limit > 0 {
			return m.msgStore.StoreMessageWithQuota(user, data, limit)
		}
	}
	return m.msgStore.StoreMessage(user, data)
}

// MDNHandler defines the interface for sending MDN notifications
type MDNHandler interface {
	SendMDN(from, to, messageID, inReplyTo string, msg []byte) error
}

// SetMDNHandler sets the handler for sending MDN notifications
func (m *BboltMailstore) SetMDNHandler(handler func(from, to, messageID, inReplyTo string, msg []byte) error) {
	m.mdnHandler = handler
}

// NewBboltMailstore creates a new mailstore backed by bbolt
func NewBboltMailstore(dataDir string) (*BboltMailstore, error) {
	// Ensure data directory exists
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	dbPath := filepath.Join(dataDir, "mail.db")
	db, err := storage.OpenDatabase(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	closeOnErr := true
	defer func() {
		if closeOnErr {
			_ = db.Close()
		}
	}()

	msgStorePath := filepath.Join(dataDir, "messages")
	msgStore, err := storage.NewMessageStore(msgStorePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create message store: %w", err)
	}

	closeOnErr = false
	return &BboltMailstore{
		dataDir:  dataDir,
		db:       db,
		msgStore: msgStore,
		mdnSent:  make(map[string]bool),
		mdnSem:   make(chan struct{}, 50),
	}, nil
}

// NewBboltMailstoreWithInterfaces creates a mailstore using existing storage instances
func NewBboltMailstoreWithInterfaces(db *storage.Database, msgStore *storage.MessageStore) *BboltMailstore {
	return &BboltMailstore{
		dataDir:  "shared",
		db:       db,
		msgStore: msgStore,
		mdnSent:  make(map[string]bool),
		mdnSem:   make(chan struct{}, 50),
	}
}

// Close closes the mailstore
func (m *BboltMailstore) Close() error {
	if m.msgStore != nil {
		_ = m.msgStore.Close()
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

// SelectMailbox returns mailbox information and ends the \Recent state of
// its messages (SELECT).
func (m *BboltMailstore) SelectMailbox(user, mailbox string) (*Mailbox, error) {
	return m.mailboxInfo(user, mailbox, true)
}

// ExamineMailbox returns the same information as SelectMailbox without
// clearing \Recent: EXAMINE and STATUS must not change the mailbox's
// permanent state (RFC 3501 §6.3.2, §6.3.10) (F5215).
func (m *BboltMailstore) ExamineMailbox(user, mailbox string) (*Mailbox, error) {
	return m.mailboxInfo(user, mailbox, false)
}

func (m *BboltMailstore) mailboxInfo(user, mailbox string, clearRecent bool) (*Mailbox, error) {
	// F5643: GetMailbox answers defaults for a name that does not exist, so
	// SELECT / EXAMINE / STATUS of a typo succeeded with an empty mailbox.
	// INBOX is case-insensitive and always exists (RFC 3501 §5.1).
	if strings.EqualFold(mailbox, "INBOX") {
		mailbox = "INBOX"
	} else if names, err := m.db.ListMailboxes(user); err != nil {
		return nil, err
	} else if !slices.Contains(names, mailbox) {
		return nil, fmt.Errorf("[NONEXISTENT] Mailbox does not exist")
	}

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

	// Clear \Recent flags when mailbox is selected (RFC 3501)
	// Messages from previous sessions should no longer be marked \Recent
	if clearRecent {
		if err := m.db.ClearRecent(user, mailbox); err != nil {
			return nil, err
		}
	}

	// Get highest modification sequence number (RFC 7162)
	highestModSeq, err := m.db.GetHighestModSeq(user, mailbox)
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
		HighestModSeq:  highestModSeq,
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

// SetSubscribed sets the subscription status of a mailbox
func (m *BboltMailstore) SetSubscribed(user, mailbox string, subscribed bool) error {
	return m.db.SetSubscribed(user, mailbox, subscribed)
}

// GetSubscribed returns the subscription status of a mailbox
func (m *BboltMailstore) GetSubscribed(user, mailbox string) (bool, error) {
	return m.db.GetSubscribed(user, mailbox)
}

// ListSubscribed lists all subscribed mailboxes for a user
func (m *BboltMailstore) ListSubscribed(user string) ([]string, error) {
	return m.db.ListSubscribed(user)
}

// matchPattern checks if a mailbox name matches an IMAP LIST pattern.
func matchPattern(name, pattern string) bool {
	return imapWildcardMatch(name, pattern)
}

// imapWildcardMatch implements RFC 3501 §6.3.8 LIST wildcards: '*' matches
// zero or more characters, '%' matches zero or more characters other than
// the "/" hierarchy delimiter; everything else matches literally. Before
// F5219 only a leading/trailing '*' was honoured, so LIST "" "%" matched
// nothing. Dynamic programming over the pattern keeps this O(len(name) *
// len(pattern)) for hostile patterns such as "*%*%*%...".
func imapWildcardMatch(name, pattern string) bool {
	// cur[i] reports whether pattern[:p] matches name[:i].
	cur := make([]bool, len(name)+1)
	next := make([]bool, len(name)+1)
	cur[0] = true
	for p := 0; p < len(pattern); p++ {
		c := pattern[p]
		for i := range next {
			next[i] = false
		}
		switch c {
		case '*', '%':
			for i := 0; i <= len(name); i++ {
				if cur[i] {
					next[i] = true
				} else if i > 0 && next[i-1] && (c == '*' || name[i-1] != '/') {
					next[i] = true
				}
			}
		default:
			for i := 1; i <= len(name); i++ {
				next[i] = cur[i-1] && name[i-1] == c
			}
		}
		cur, next = next, cur
	}
	return cur[len(name)]
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
	uidCount := len(uids)
	if uidCount > 0x7FFFFFFF {
		return nil, fmt.Errorf("mailbox exceeds maximum message count")
	}
	total := uint32(uidCount)
	for i, uid := range uids {
		// IMAP uses 1-based sequence numbers
		seqNum := uint32(i + 1)

		// Check if this sequence number is in the requested set
		inSet := false
		for _, r := range ranges {
			if r.Contains(seqNum, total) {
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
	needsData, marksSeen := false, false
	for _, item := range items {
		item = strings.ToUpper(item)
		if item == "RFC822" || item == "BODY" || strings.HasPrefix(item, "BODY[") || strings.HasPrefix(item, "BODY.PEEK[") {
			needsData = true
		}
		if item == "RFC822" || strings.HasPrefix(item, "BODY[") {
			marksSeen = true
		}
	}

	if needsData {
		data, err := m.msgStore.ReadMessage(user, meta.MessageID)
		if err == nil {
			msg.Data = data
			// F5644: only an item that reads the message (and sets \Seen)
			// may trigger the read receipt; BODY.PEEK[...] and BODYSTRUCTURE
			// are side-effect free (RFC 3501 §6.4.5, RFC 8098 §2.1).
			if marksSeen {
				m.checkAndSendMDN(user, meta.MessageID, meta.From, meta.To, data)
			}
		}
	}

	return msg, nil
}

// checkAndSendMDN checks if an MDN should be sent for this message
func (m *BboltMailstore) checkAndSendMDN(user, messageID, from, to string, msgData []byte) {
	// Capture handler under lock to avoid race with SetMDNHandler
	m.mdnSentMu.Lock()
	handler := m.mdnHandler
	if handler == nil {
		m.mdnSentMu.Unlock()
		return
	}

	// Check if MDN already sent for this message
	if m.mdnSent == nil {
		m.mdnSent = make(map[string]bool)
	}
	if m.mdnSent[messageID] {
		m.mdnSentMu.Unlock()
		return
	}
	m.mdnSent[messageID] = true
	m.mdnSentMu.Unlock()

	// Parse message to check for Disposition-Notification-To header
	header := parseDispositionHeader(string(msgData))
	if header == "" {
		return
	}

	// Parse the MDN address
	mdnTo, err := parseMDNAddress(header)
	if err != nil || mdnTo == "" {
		return
	}

	// Extract In-Reply-To for the MDN
	inReplyTo := messageID

	// Send MDN asynchronously using captured handler
	select {
	case m.mdnSem <- struct{}{}:
		go func() {
			defer func() {
				<-m.mdnSem
				recover() // Silently swallow panic to avoid crashing the server
			}()
			if err := handler(from, mdnTo, messageID, inReplyTo, msgData); err != nil {
				// Log error but don't fail the fetch
				m.mdnSentMu.Lock()
				delete(m.mdnSent, messageID) // Allow retry on failure
				m.mdnSentMu.Unlock()
			}
		}()
	default:
		// Semaphore full; drop MDN to bound concurrency
	}
}

// parseDispositionHeader extracts the Disposition-Notification-To field value
// from a message. The field name is matched case-insensitively (F5645: the
// canonical mixed-case spelling was never stripped).
func parseDispositionHeader(msgStr string) string {
	for _, line := range strings.Split(msgStr, "\r\n") {
		if line == "" {
			break // end of the header section
		}
		if len(line) > len("disposition-notification-to:") && strings.EqualFold(line[:len("disposition-notification-to:")], "disposition-notification-to:") {
			return strings.TrimSpace(line[len("disposition-notification-to:"):])
		}
	}
	return ""
}

// parseMDNAddress extracts the first email address from Disposition-Notification-To
func parseMDNAddress(header string) (string, error) {
	header = strings.TrimSpace(header)
	if list, err := mail.ParseAddressList(header); err == nil && len(list) > 0 && strings.Contains(list[0].Address, "@") {
		return list[0].Address, nil
	}
	// Remove angle brackets if present
	if strings.HasPrefix(header, "<") && strings.HasSuffix(header, ">") {
		header = header[1 : len(header)-1]
	}
	// If it contains @, it's likely an email
	if strings.Contains(header, "@") {
		return header, nil
	}
	return "", fmt.Errorf("invalid MDN address")
}

// StoreFlags updates message flags
// FlagOperation represents the type of flag operation
type FlagOperation int

const (
	FlagAdd     FlagOperation = iota // +FLAGS
	FlagRemove                       // -FLAGS
	FlagReplace                      // FLAGS (replace all)
)

func (op FlagOperation) String() string {
	switch op {
	case FlagAdd:
		return "add"
	case FlagRemove:
		return "remove"
	case FlagReplace:
		return "replace"
	default:
		return "unknown"
	}
}

// StoreFlags updates flags for messages in a mailbox
func (m *BboltMailstore) StoreFlags(user, mailbox string, seqSet string, flags []string, op FlagOperation) error {
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
	uidCount := len(uids)
	if uidCount > 0x7FFFFFFF {
		return fmt.Errorf("mailbox exceeds maximum message count")
	}
	total := uint32(uidCount)

	for i, uid := range uids {
		// IMAP uses 1-based sequence numbers
		seqNum := uint32(i + 1)

		// Check if in set
		inSet := false
		for _, r := range ranges {
			if r.Contains(seqNum, total) {
				inSet = true
				break
			}
		}

		if !inSet {
			continue
		}

		// Atomically update flags to prevent lost updates under concurrent access
		var updatedFlags []string
		err = m.db.UpdateMessageMetadataFunc(user, mailbox, uid, func(meta *storage.MessageMetadata) error {
			switch op {
			case FlagAdd:
				for _, flag := range flags {
					if !hasFlag(meta.Flags, flag) {
						meta.Flags = append(meta.Flags, flag)
					}
				}
			case FlagRemove:
				var newFlags []string
				for _, f := range meta.Flags {
					if !hasFlag(flags, f) {
						newFlags = append(newFlags, f)
					}
				}
				meta.Flags = newFlags
			case FlagReplace:
				meta.Flags = flags
			}
			updatedFlags = meta.Flags
			return nil
		})
		if err != nil {
			continue
		}

		// Notify about flag changes
		GetNotificationHub().NotifyFlagsChanged(user, mailbox, uid, seqNum, updatedFlags)
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

	var released []string
	for _, uid := range uids {
		meta, err := m.db.GetMessageMetadata(user, mailbox, uid)
		if err != nil {
			continue
		}

		// Check if deleted
		if hasFlag(meta.Flags, "\\Deleted") {
			if m.db.DeleteMessage(user, mailbox, uid) == nil {
				released = append(released, meta.MessageID)
			}
		}
	}

	m.releaseBlobs(user, released)
	return nil
}

// ExpungeUIDs removes only the messages whose UID is in uids and that carry
// \Deleted (RFC 4315 UID EXPUNGE; also used to complete RFC 6851 MOVE).
func (m *BboltMailstore) ExpungeUIDs(user, mailbox string, uids []uint32) error {
	var released []string
	for _, uid := range uids {
		meta, err := m.db.GetMessageMetadata(user, mailbox, uid)
		if err != nil {
			continue
		}
		if hasFlag(meta.Flags, "\\Deleted") {
			if m.db.DeleteMessage(user, mailbox, uid) == nil {
				released = append(released, meta.MessageID)
			}
		}
	}
	m.releaseBlobs(user, released)
	return nil
}

// releaseBlobs deletes the message files in ids that no metadata row of user
// still references. Blobs are content-addressed (sha256 of the bytes), so a
// COPY/MOVE destination or any identical message shares the file with the
// expunged one (F5065). When the reference scan fails the files are kept:
// leaking a blob is recoverable, deleting a live one is not.
func (m *BboltMailstore) releaseBlobs(user string, ids []string) {
	if len(ids) == 0 {
		return
	}
	mailboxes, err := m.db.ListMailboxes(user)
	if err != nil {
		return
	}
	candidates := make(map[string]bool, len(ids))
	for _, id := range ids {
		candidates[id] = true
	}
	for _, mb := range mailboxes {
		uids, err := m.db.GetMessageUIDs(user, mb)
		if err != nil {
			return
		}
		for _, uid := range uids {
			meta, err := m.db.GetMessageMetadata(user, mb, uid)
			if err != nil {
				return
			}
			delete(candidates, meta.MessageID)
		}
		if len(candidates) == 0 {
			return
		}
	}
	for id := range candidates {
		_ = m.msgStore.DeleteMessage(user, id)
	}
}

// parseMessageHeadersExtended extracts headers including threading info
func parseMessageHeadersExtended(data []byte) (subject, from, to, date, msgID, inReplyTo string, references []string) {
	// First get basic headers
	subject, from, to, date = parseMessageHeaders(data)

	// Parse headers for threading info
	headers := string(data)
	if idx := strings.Index(headers, "\r\n\r\n"); idx != -1 {
		headers = headers[:idx]
	} else if idx := strings.Index(headers, "\n\n"); idx != -1 {
		headers = headers[:idx]
	}

	for _, line := range strings.Split(headers, "\n") {
		line = strings.TrimRight(line, "\r")
		lower := strings.ToLower(line)

		if strings.HasPrefix(lower, "message-id:") {
			msgID = strings.TrimSpace(line[11:])
			// Remove angle brackets if present
			msgID = strings.Trim(msgID, "<>")
		} else if strings.HasPrefix(lower, "in-reply-to:") {
			inReplyTo = strings.TrimSpace(line[12:])
			// Remove angle brackets if present
			inReplyTo = strings.Trim(inReplyTo, "<>")
		} else if strings.HasPrefix(lower, "references:") {
			refs := strings.TrimSpace(line[11:])
			// Parse reference IDs (space-separated, angle-bracket wrapped)
			for _, ref := range strings.Fields(refs) {
				ref = strings.Trim(ref, "<>")
				if ref != "" {
					references = append(references, ref)
				}
			}
		}
	}

	return subject, from, to, date, msgID, inReplyTo, references
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

// AppendMessage appends a message to a mailbox
func (m *BboltMailstore) AppendMessage(user, mailbox string, flags []string, date time.Time, data []byte) error {
	_, _, err := m.AppendMessageUID(user, mailbox, flags, date, data)
	return err
}

// AppendMessageUID appends like AppendMessage and returns the mailbox's
// UIDVALIDITY with the UID assigned, for the RFC 4315 APPENDUID response
// code (F5641).
func (m *BboltMailstore) AppendMessageUID(user, mailbox string, flags []string, date time.Time, data []byte) (uint32, uint32, error) {
	// RFC 3629: validate UTF-8 well-formedness before storing
	if !utf8.Valid(data) {
		return 0, 0, fmt.Errorf("message contains invalid UTF-8 sequence")
	}

	// Store message
	messageID, err := m.storeBlob(user, data)
	if err != nil {
		return 0, 0, err
	}

	// Get next UID
	uid, err := m.db.GetNextUID(user, mailbox)
	if err != nil {
		return 0, 0, err
	}

	// Parse basic headers for indexing
	subject, from, to, dateStr, _, inReplyTo, references := parseMessageHeadersExtended(data)

	// Get or create thread ID
	threadID, err := m.db.GetOrCreateThreadID(user, mailbox, subject, inReplyTo, references)
	if err != nil {
		threadID = "" // Continue without threading if it fails
	}

	// Check if this is the root of the thread
	isThreadRoot := inReplyTo == "" && len(references) == 0
	if !isThreadRoot && threadID != "" {
		// Check if there's already a message with this thread ID
		existingMsgs, _ := m.db.GetThreadMessages(user, mailbox, threadID)
		isThreadRoot = len(existingMsgs) == 0
	}

	// Ensure \Recent flag is set for newly appended messages (RFC 3501)
	if !storage.HasFlag(flags, "\\Recent") {
		flags = append(flags, "\\Recent")
	}

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
		InReplyTo:    inReplyTo,
		References:   references,
		ThreadID:     threadID,
		IsThreadRoot: isThreadRoot,
	}

	if err := m.db.StoreMessageMetadata(user, mailbox, uid, meta); err != nil {
		return 0, 0, err
	}

	// Update thread information
	if threadID != "" {
		m.updateThreadInfo(user, mailbox, threadID, subject, from, meta)
	}

	// Get the sequence number for the new message
	uids, err := m.db.GetMessageUIDs(user, mailbox)
	if err == nil {
		uidCount := len(uids)
		if uidCount > 0x7FFFFFFF {
			return 0, 0, fmt.Errorf("mailbox exceeds maximum message count")
		}
		seqNum := uint32(uidCount)
		// Notify subscribers about the new message
		GetNotificationHub().NotifyNewMessage(user, mailbox, uid, seqNum)
	}

	mb, err := m.db.GetMailbox(user, mailbox)
	if err != nil {
		return 0, uid, nil // stored; only the APPENDUID code is lost
	}
	return mb.UIDValidity, uid, nil
}

// updateThreadInfo updates the thread summary information
func (m *BboltMailstore) updateThreadInfo(user, mailbox, threadID, subject, from string, meta *storage.MessageMetadata) {
	// Get or create thread
	thread, err := m.db.GetThread(user, threadID)
	if err != nil {
		// Create new thread
		thread = &storage.Thread{
			ThreadID:     threadID,
			Subject:      storage.NormalizeSubject(subject),
			Participants: []string{from},
			MessageCount: 0,
			UnreadCount:  0,
			LastActivity: meta.InternalDate,
			CreatedAt:    time.Now(),
		}
	}

	// Update thread info
	thread.MessageCount++
	thread.LastActivity = meta.InternalDate

	// Check if sender is already in participants
	found := false
	for _, p := range thread.Participants {
		if p == from {
			found = true
			break
		}
	}
	if !found {
		thread.Participants = append(thread.Participants, from)
	}

	// Update unread count
	if !storage.HasFlag(meta.Flags, "\\Seen") {
		thread.UnreadCount++
	}

	// Save thread
	_ = m.db.UpdateThread(user, thread)
}

// SearchMessages searches for messages matching criteria
func (m *BboltMailstore) SearchMessages(user, mailbox string, criteria SearchCriteria) ([]uint32, error) {
	// Get all message UIDs
	uids, err := m.db.GetMessageUIDs(user, mailbox)
	if err != nil {
		return nil, err
	}

	// RFC 3501 §6.4.4: the sequence-set and UID <set> keys restrict the
	// result like every other key; "*" is the last message / highest UID
	// (F5218).
	seqRanges, err := ParseSequenceSet(criteria.SeqSet)
	if err != nil {
		return nil, err
	}
	uidRanges, err := ParseSequenceSet(criteria.UIDSet)
	if err != nil {
		return nil, err
	}
	maxSeq := uint32(len(uids))
	var maxUID uint32
	for _, uid := range uids {
		if uid > maxUID {
			maxUID = uid
		}
	}

	var results []uint32
	seqNum := uint32(0)

	for _, uid := range uids {
		seqNum++

		if criteria.SeqSet != "" && !inSeqRanges(seqRanges, seqNum, maxSeq) {
			continue
		}
		if criteria.UIDSet != "" && !inSeqRanges(uidRanges, uid, maxUID) {
			continue
		}

		meta, err := m.db.GetMessageMetadata(user, mailbox, uid)
		if err != nil {
			continue
		}

		// Load message data for content-based criteria (CC, BCC, BODY, TEXT, HEADER)
		var msgData []byte
		if criteria.Cc != "" || criteria.Bcc != "" || criteria.Body != "" || criteria.Text != "" || len(criteria.Header) > 0 {
			msgData, err = m.msgStore.ReadMessage(user, meta.MessageID)
			if err != nil {
				continue
			}
		}

		// Check criteria
		if matchesCriteria(meta, msgData, &criteria) {
			results = append(results, seqNum)
		}
	}

	return results, nil
}

// inSeqRanges reports whether n falls in any of ranges, with "*" = max.
func inSeqRanges(ranges []SeqRange, n, hi uint32) bool {
	for _, r := range ranges {
		if r.Contains(n, hi) {
			return true
		}
	}
	return false
}

// matchesCriteria checks if a message matches search criteria
func matchesCriteria(meta *storage.MessageMetadata, msgData []byte, criteria *SearchCriteria) bool {
	// Handle NOT
	if criteria.Not != nil {
		if matchesCriteria(meta, msgData, criteria.Not) {
			return false
		}
	}

	// Handle OR
	if criteria.Or[0] != nil && criteria.Or[1] != nil {
		if !matchesCriteria(meta, msgData, criteria.Or[0]) && !matchesCriteria(meta, msgData, criteria.Or[1]) {
			return false
		}
	}

	// Check flag criteria.
	//
	// The ALL criterion (and an empty search) matches every message, so it is a
	// no-op here: it is honoured by simply falling through, because with no
	// other criterion set nothing rejects the message (the function returns
	// true at the end). We must NOT short-circuit on All here — parseSearchCriteria
	// seeds All:true for every parsed search, so an early `return true` on All
	// would make every SEARCH (UNSEEN, SEEN, FROM ...) match every message.
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
	if criteria.New && (!hasFlag(meta.Flags, "\\Recent") || hasFlag(meta.Flags, "\\Seen")) {
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

	// Check internal date criteria
	if !criteria.Before.IsZero() && !meta.InternalDate.Before(criteria.Before) {
		return false
	}
	if !criteria.On.IsZero() {
		metaDate := time.Date(meta.InternalDate.Year(), meta.InternalDate.Month(), meta.InternalDate.Day(), 0, 0, 0, 0, meta.InternalDate.Location())
		critDate := time.Date(criteria.On.Year(), criteria.On.Month(), criteria.On.Day(), 0, 0, 0, 0, criteria.On.Location())
		if !metaDate.Equal(critDate) {
			return false
		}
	}
	if !criteria.Since.IsZero() && meta.InternalDate.Before(criteria.Since) {
		return false
	}

	// Check sent date criteria: the Date header's own calendar date,
	// disregarding time and timezone (RFC 3501 §6.4.4). F5562: a missing or
	// unparsable Date matched every SENT* key, and the instant was compared
	// in UTC, so a 23:00 -0500 message fell on the next day.
	if !criteria.SentBefore.IsZero() || !criteria.SentOn.IsZero() || !criteria.SentSince.IsZero() {
		sent, err := parseMessageDate(meta.Date)
		if err != nil {
			return false
		}
		day := time.Date(sent.Year(), sent.Month(), sent.Day(), 0, 0, 0, 0, time.UTC)
		dayOf := func(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC) }
		if !criteria.SentBefore.IsZero() && !day.Before(dayOf(criteria.SentBefore)) {
			return false
		}
		if !criteria.SentOn.IsZero() && !day.Equal(dayOf(criteria.SentOn)) {
			return false
		}
		if !criteria.SentSince.IsZero() && day.Before(dayOf(criteria.SentSince)) {
			return false
		}
	}

	// Check content-based criteria (requires message data)
	if msgData != nil {
		msgStr := strings.ToLower(string(msgData))

		// CC criteria
		if criteria.Cc != "" {
			msg, err := mail.ReadMessage(strings.NewReader(string(msgData)))
			if err != nil || !strings.Contains(strings.ToLower(msg.Header.Get("Cc")), strings.ToLower(criteria.Cc)) {
				return false
			}
		}

		// BCC criteria (not actually in received messages, but we check headers before send)
		// BCC is never visible to recipients, so we check the raw envelope
		if criteria.Bcc != "" {
			// BCC is typically removed during delivery, but for search we check if it was ever present
			// This would require examining the original SMTP envelope, not the message itself
			// For now, we return false as BCC cannot be determined from the message content
			return false
		}

		// BODY criteria - search entire message body (after headers)
		if criteria.Body != "" {
			// Find headers separator
			headerEnd := strings.Index(msgStr, "\r\n\r\n")
			if headerEnd == -1 {
				headerEnd = strings.Index(msgStr, "\n\n")
			}
			bodyStr := ""
			if headerEnd != -1 {
				bodyStr = msgStr[headerEnd:]
			} else {
				bodyStr = msgStr
			}
			if !strings.Contains(bodyStr, strings.ToLower(criteria.Body)) {
				return false
			}
		}

		// TEXT criteria - search entire message (headers + body)
		if criteria.Text != "" {
			if !strings.Contains(msgStr, strings.ToLower(criteria.Text)) {
				return false
			}
		}

		// HEADER criteria - search specific header
		for headerName, headerValue := range criteria.Header {
			headerLine := strings.ToLower(headerName) + ":"
			headerIdx := strings.Index(msgStr, "\r\n"+headerLine)
			if headerIdx == -1 {
				headerIdx = strings.Index(msgStr, "\n"+headerLine)
			}
			if headerIdx == -1 {
				headerIdx = strings.Index(msgStr, headerLine) // For first header without leading newline
			}
			if headerIdx == -1 {
				return false
			}
			// Extract header value
			valueStart := headerIdx + len(headerLine)
			lineEnd := strings.Index(msgStr[valueStart:], "\r\n")
			if lineEnd == -1 {
				lineEnd = strings.Index(msgStr[valueStart:], "\n")
			}
			if lineEnd == -1 {
				lineEnd = len(msgStr)
			}
			headerVal := strings.TrimSpace(msgStr[valueStart : valueStart+lineEnd])
			if !strings.Contains(headerVal, strings.ToLower(headerValue)) {
				return false
			}
		}
	}

	return true
}

// parseMessageDate parses an email Date header (RFC 2822 format)
// Format: "Mon, 01 Jan 2024 10:00:00 +0000" or similar variants
func parseMessageDate(dateStr string) (time.Time, error) {
	// RFC 5322 (one- or two-digit day, optional weekday, obsolete zones,
	// trailing comment). F5562: only "02" days parsed, so the common
	// "Tue, 2 Jan 2024 ..." form was treated as having no date.
	if t, err := mail.ParseDate(dateStr); err == nil {
		return t, nil
	}
	// Try RFC 2822 format first
	t, err := time.Parse("Mon, 02 Jan 2006 15:04:05 -0700", dateStr)
	if err == nil {
		return t, nil
	}
	// Try without timezone
	t, err = time.Parse("Mon, 02 Jan 2006 15:04:05", dateStr)
	if err == nil {
		return t, nil
	}
	// Try date only
	t, err = time.Parse("Mon, 02 Jan 2006", dateStr)
	if err == nil {
		return t, nil
	}
	// Try ISO-like format
	t, err = time.Parse("02 Jan 2006 15:04:05 -0700", dateStr)
	if err == nil {
		return t, nil
	}
	// Fallback to basic date
	return time.Parse("02-Jan-2006", dateStr)
}

// CopyMessages copies messages to another mailbox
func (m *BboltMailstore) CopyMessages(user, sourceMailbox, destMailbox string, seqSet string) error {
	_, _, _, err := m.CopyMessagesUIDs(user, sourceMailbox, destMailbox, seqSet)
	return err
}

// CopyMessagesUIDs copies like CopyMessages and returns the destination
// UIDVALIDITY with the source and destination UIDs of every copied message,
// in the same order, for the RFC 4315 COPYUID response code (F5564).
func (m *BboltMailstore) CopyMessagesUIDs(user, sourceMailbox, destMailbox string, seqSet string) (uint32, []uint32, []uint32, error) {
	// Parse sequence set
	ranges, err := ParseSequenceSet(seqSet)
	if err != nil {
		return 0, nil, nil, err
	}

	// Get source message UIDs
	uids, err := m.db.GetMessageUIDs(user, sourceMailbox)
	if err != nil {
		return 0, nil, nil, err
	}
	uidCount := len(uids)
	if uidCount > 0x7FFFFFFF {
		return 0, nil, nil, fmt.Errorf("mailbox exceeds maximum message count")
	}
	total := uint32(uidCount)

	var srcUIDs, dstUIDs []uint32
	var newIDs []string
	// F5640: a message that cannot be copied fails the whole COPY. The old
	// loop skipped it and answered OK, and MOVE then deleted the source
	// message it had never copied. Copies already made are rolled back
	// (RFC 3501 §6.4.7: an unsuccessful COPY leaves the destination as it was).
	fail := func(uid uint32, err error) (uint32, []uint32, []uint32, error) {
		for _, dstUID := range dstUIDs {
			_ = m.db.DeleteMessage(user, destMailbox, dstUID)
		}
		m.releaseBlobs(user, newIDs)
		return 0, nil, nil, fmt.Errorf("copy of UID %d failed: %w", uid, err)
	}
	for i, uid := range uids {
		seqNum := uint32(i + 1) // IMAP uses 1-based sequence numbers
		// Check if in set
		inSet := false
		for _, r := range ranges {
			if r.Contains(seqNum, total) {
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
			return fail(uid, err)
		}

		// Get message data
		data, err := m.msgStore.ReadMessage(user, meta.MessageID)
		if err != nil {
			return fail(uid, err)
		}

		// Get next UID for destination
		newUID, err := m.db.GetNextUID(user, destMailbox)
		if err != nil {
			return fail(uid, err)
		}

		// Copy message
		newMessageID, err := m.storeBlob(user, data)
		if err != nil {
			return fail(uid, err)
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

		if err := m.db.StoreMessageMetadata(user, destMailbox, newUID, newMeta); err != nil {
			newIDs = append(newIDs, newMessageID)
			return fail(uid, err)
		}
		srcUIDs, dstUIDs = append(srcUIDs, uid), append(dstUIDs, newUID)
		newIDs = append(newIDs, newMessageID)
	}

	mb, err := m.db.GetMailbox(user, destMailbox)
	if err != nil {
		return 0, srcUIDs, dstUIDs, nil // copied; only the COPYUID code is lost
	}
	return mb.UIDValidity, srcUIDs, dstUIDs, nil
}

// MessageHeader returns the header section of the message with the given
// UID, without FETCH side effects (no \Seen, no MDN), for THREAD and SORT CC.
func (m *BboltMailstore) MessageHeader(user, mailbox string, uid uint32) ([]byte, error) {
	meta, err := m.db.GetMessageMetadata(user, mailbox, uid)
	if err != nil {
		return nil, err
	}
	data, err := m.msgStore.ReadMessage(user, meta.MessageID)
	if err != nil {
		return nil, err
	}
	if i := strings.Index(string(data), "\r\n\r\n"); i >= 0 {
		return data[:i+2], nil
	}
	if i := strings.Index(string(data), "\n\n"); i >= 0 {
		return data[:i+1], nil
	}
	return data, nil
}

// MoveMessages moves messages to another mailbox
func (m *BboltMailstore) MoveMessages(user, sourceMailbox, destMailbox string, seqSet string) error {
	_, _, _, err := m.MoveMessagesUIDs(user, sourceMailbox, destMailbox, seqSet)
	return err
}

// MoveMessagesUIDs copies the messages and then flags exactly the source
// messages that were copied \Deleted, returning the destination UIDVALIDITY
// and the source / destination UIDs for the RFC 6851 COPYUID response (F5641).
// F5640: it used to flag the whole requested set whether or not each message
// had been copied.
func (m *BboltMailstore) MoveMessagesUIDs(user, sourceMailbox, destMailbox string, seqSet string) (uint32, []uint32, []uint32, error) {
	// CopyMessagesUIDs works from a snapshot of the source UIDs, so the new
	// copies are never flagged when source == destination.
	validity, srcUIDs, dstUIDs, err := m.CopyMessagesUIDs(user, sourceMailbox, destMailbox, seqSet)
	if err != nil {
		return 0, nil, nil, err
	}

	// Then mark the copied originals as deleted
	for _, uid := range srcUIDs {
		meta, err := m.db.GetMessageMetadata(user, sourceMailbox, uid)
		if err != nil {
			return 0, nil, nil, fmt.Errorf("flag moved UID %d: %w", uid, err)
		}

		// Add deleted flag
		if !hasFlag(meta.Flags, "\\Deleted") {
			meta.Flags = append(meta.Flags, "\\Deleted")
			if err := m.db.UpdateMessageMetadata(user, sourceMailbox, uid, meta); err != nil {
				return 0, nil, nil, fmt.Errorf("flag moved UID %d: %w", uid, err)
			}
		}
	}

	return validity, srcUIDs, dstUIDs, nil
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

// ---------------------------------------------------------------------------
// ACL (Access Control List) — RFC 4314
// ---------------------------------------------------------------------------

// GetACL retrieves the rights a grantee has on a specific mailbox.
func (m *BboltMailstore) GetACL(owner, mailbox, grantee string) (uint8, error) {
	if m.db == nil {
		return 0, nil
	}
	rights, err := m.db.GetACL(owner, mailbox, grantee)
	return uint8(rights), err
}

// SetACL sets or updates an ACL entry.
func (m *BboltMailstore) SetACL(owner, mailbox, grantee string, rights uint8, grantingUser string) error {
	if m.db == nil {
		return nil
	}
	return m.db.SetACL(owner, mailbox, grantee, storage.ACLRights(rights), grantingUser)
}

// DeleteACL removes ACL entries.
func (m *BboltMailstore) DeleteACL(owner, mailbox, grantee string) error {
	if m.db == nil {
		return nil
	}
	return m.db.DeleteACL(owner, mailbox, grantee)
}

// ListACL returns all ACL entries for a mailbox.
func (m *BboltMailstore) ListACL(owner, mailbox string) ([]storage.ACLEntry, error) {
	if m.db == nil {
		return nil, nil
	}
	return m.db.ListACL(owner, mailbox)
}

// ListMailboxesSharedWith returns mailboxes shared with the given user.
func (m *BboltMailstore) ListMailboxesSharedWith(user string) ([]string, error) {
	if m.db == nil {
		return nil, nil
	}
	return m.db.ListMailboxesSharedWith(user)
}

// ListGranteesMailboxes returns mailboxes owned by owner that are shared.
func (m *BboltMailstore) ListGranteesMailboxes(owner string) ([]string, error) {
	if m.db == nil {
		return nil, nil
	}
	return m.db.ListGranteesMailboxes(owner)
}

// ---------------------------------------------------------------------------
// Default Mailbox Provisioning
// ---------------------------------------------------------------------------

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
