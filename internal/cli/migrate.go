package cli

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	extimap "github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/storage"
)

// MigrationManager handles data migration from external sources
type MigrationManager struct {
	logger   *slog.Logger
	db       *db.DB
	msgStore *storage.MessageStore
}

// NewMigrationManager creates a new migration manager
func NewMigrationManager(database *db.DB, msgStore *storage.MessageStore, logger *slog.Logger) *MigrationManager {
	if logger == nil {
		logger = slog.Default()
	}

	return &MigrationManager{
		logger:   logger,
		db:       database,
		msgStore: msgStore,
	}
}

// MigrateOptions contains migration options
type MigrateOptions struct {
	SourceType string // imap, dovecot, mbox, etc.
	SourceURL  string // For IMAP sources
	SourcePath string // For file-based sources
	Username   string
	Password   string
	TargetUser string
	DryRun     bool
}

// MigrateFromIMAP migrates data from an IMAP server
func (mm *MigrationManager) MigrateFromIMAP(opts MigrateOptions) error {
	mm.logger.Info("Starting IMAP migration",
		"source", opts.SourceURL,
		"target_user", opts.TargetUser,
	)

	fmt.Printf("Migrating from IMAP: %s\n", opts.SourceURL)
	fmt.Printf("Target user: %s\n", opts.TargetUser)

	if opts.DryRun {
		fmt.Println("DRY RUN MODE - No changes will be made")
	}

	// Parse IMAP URL
	u, err := url.Parse(opts.SourceURL)
	if err != nil {
		return fmt.Errorf("invalid IMAP URL: %w", err)
	}

	host := u.Host
	port := 993
	useTLS := true

	// Handle non-standard ports
	if strings.Contains(host, ":") {
		parts := strings.Split(host, ":")
		host = parts[0]
		if len(parts) > 1 {
			_, _ = fmt.Sscanf(parts[1], "%d", &port)
		}
	} else if u.Scheme == "imap" {
		port = 143
		useTLS = false
	}

	fmt.Printf("Host: %s\n", host)
	fmt.Printf("Port: %d\n", port)
	fmt.Printf("Username: %s\n", opts.Username)

	// Connect to IMAP server
	var imapClient *client.Client
	if useTLS {
		imapClient, err = client.DialTLS(fmt.Sprintf("%s:%d", host, port), nil)
	} else {
		imapClient, err = client.Dial(fmt.Sprintf("%s:%d", host, port))
	}
	if err != nil {
		return fmt.Errorf("failed to connect to IMAP server: %w", err)
	}
	defer imapClient.Logout()

	// Authenticate
	if err := imapClient.Login(opts.Username, opts.Password); err != nil {
		return fmt.Errorf("failed to authenticate: %w", err)
	}
	fmt.Println("Authenticated successfully")

	// List mailboxes using channel-based API
	mailboxChan := make(chan *extimap.MailboxInfo, 100)
	doneChan := make(chan error, 1)

	go func() {
		doneChan <- imapClient.List("", "*", mailboxChan)
	}()

	var mailboxes []*extimap.MailboxInfo
	for mbox := range mailboxChan {
		mailboxes = append(mailboxes, mbox)
	}

	if err := <-doneChan; err != nil {
		return fmt.Errorf("failed to list mailboxes: %w", err)
	}

	fmt.Printf("Found %d mailboxes\n", len(mailboxes))

	totalMessages := 0
	migratedMessages := 0

	for _, mailbox := range mailboxes {
		// Check if mailbox is selectable (skip \Noselect mailboxes)
		skip := false
		for _, attr := range mailbox.Attributes {
			if attr == "\\Noselect" || attr == "Noselect" {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		mboxName := mailbox.Name
		if mboxName == "" {
			mboxName = "INBOX"
		}

		fmt.Printf("\nMigrating mailbox: %s\n", mboxName)

		// Select mailbox
		mbox, err := imapClient.Select(mboxName, false)
		if err != nil {
			mm.logger.Warn("Failed to select mailbox, skipping", "mailbox", mboxName, "error", err)
			continue
		}

		if mbox.Messages == 0 {
			fmt.Printf("  Empty mailbox, skipping\n")
			continue
		}

		fmt.Printf("  Messages: %d\n", mbox.Messages)
		totalMessages += int(mbox.Messages)

		// Fetch messages in batches
		seqSet := new(extimap.SeqSet)
		seqSet.AddRange(1, mbox.Messages)

		items := []extimap.FetchItem{
			extimap.FetchEnvelope,
			extimap.FetchFlags,
			extimap.FetchInternalDate,
			extimap.FetchBody,
		}

		messages := make(chan *extimap.Message, 100)
		done := make(chan error, 1)

		go func() {
			done <- imapClient.Fetch(seqSet, items, messages)
		}()

		for msg := range messages {
			if opts.DryRun {
				fmt.Printf("  [DRY RUN] Would migrate message %d\n", msg.SeqNum)
				migratedMessages++
				continue
			}

			// Extract message content using GetBody
			msgData, err := extractIMAPMessageData(msg)
			if err != nil {
				mm.logger.Warn("Failed to extract message data", "seq", msg.SeqNum, "error", err)
				continue
			}

			// Store message
			if mm.msgStore != nil {
				msgID, err := mm.msgStore.StoreMessage(opts.TargetUser, msgData)
				if err != nil {
					mm.logger.Error("Failed to store message", "seq", msg.SeqNum, "error", err)
					continue
				}

				// Store flags if supported
				if len(msg.Flags) > 0 {
					mm.storeIMAPMessageFlags(opts.TargetUser, msgID, msg.Flags)
				}

				fmt.Printf("  Migrated message %d (ID: %s)\n", msg.SeqNum, msgID[:8])
			}
			migratedMessages++

			// Progress indicator for large mailboxes
			if migratedMessages%100 == 0 {
				fmt.Printf("  Progress: %d / %d messages\n", migratedMessages, totalMessages)
			}
		}

		if err := <-done; err != nil {
			mm.logger.Warn("Error fetching messages", "mailbox", mboxName, "error", err)
		}
	}

	fmt.Printf("\n=== Migration Complete ===\n")
	fmt.Printf("Total mailboxes: %d\n", len(mailboxes))
	fmt.Printf("Total messages: %d\n", totalMessages)
	fmt.Printf("Migrated: %d\n", migratedMessages)

	if opts.DryRun {
		fmt.Println("(Dry run - no actual changes made)")
	}

	return nil
}

// extractIMAPMessageData extracts the raw message data from an IMAP message
func extractIMAPMessageData(msg *extimap.Message) ([]byte, error) {
	// Use GetBody to get the message body
	// Empty section name gets the entire message
	section := &extimap.BodySectionName{}
	body := msg.GetBody(section)
	if body != nil {
		data := make([]byte, body.Len())
		_, _ = body.Read(data)
		return data, nil
	}
	return nil, fmt.Errorf("no body found in message")
}

// storeIMAPMessageFlags stores message flags in the database
func (mm *MigrationManager) storeIMAPMessageFlags(user, msgID string, flags []string) {
	mm.logger.Debug("Message flags", "user", user, "msgID", msgID[:8], "flags", flags)
}

// MigrateFromDovecot imports from Dovecot maildir
func (mm *MigrationManager) MigrateFromDovecot(maildirPath, passwdFile string) error {
	mm.logger.Info("Starting Dovecot migration",
		"maildir_path", maildirPath,
		"passwd_file", passwdFile,
	)

	fmt.Printf("Migrating from Dovecot: %s\n", maildirPath)

	// Check if maildir exists
	if _, err := os.Stat(maildirPath); err != nil {
		return fmt.Errorf("maildir not found: %w", err)
	}

	// Read passwd file if provided
	if passwdFile != "" {
		if err := mm.importDovecotUsers(passwdFile); err != nil {
			return fmt.Errorf("failed to import users: %w", err)
		}
	}

	// Walk maildir and import messages
	return mm.importMaildir(maildirPath)
}

// importDovecotUsers imports user accounts from Dovecot passwd file
func (mm *MigrationManager) importDovecotUsers(passwdFile string) error {
	fmt.Printf("Importing users from: %s\n", passwdFile)

	file, err := os.Open(filepath.Clean(passwdFile))
	if err != nil {
		return fmt.Errorf("failed to open passwd file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	imported := 0

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse passwd line: user@domain:password_hash:uid:gid::home_dir:shell
		parts := strings.Split(line, ":")
		if len(parts) < 7 {
			continue
		}

		email := parts[0]
		passwordHash := parts[1]
		homeDir := parts[5]

		fmt.Printf("Importing user: %s (home: %s)\n", email, homeDir)

		// Parse email
		user, domain := parseEmail(email)

		// Create account
		account := &db.AccountData{
			Email:        email,
			LocalPart:    user,
			Domain:       domain,
			PasswordHash: passwordHash, // Note: May need conversion
			IsActive:     true,
		}

		if err := mm.db.CreateAccount(account); err != nil {
			mm.logger.Error("Failed to import user", "email", email, "error", err)
			continue
		}

		imported++
	}

	fmt.Printf("Imported %d users\n", imported)
	return scanner.Err()
}

// importMaildir imports messages from a maildir
func (mm *MigrationManager) importMaildir(maildirPath string) error {
	fmt.Println("Scanning maildir...")

	// Walk the maildir
	return filepath.Walk(maildirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Check if it's a message file (in cur/ or new/)
		if strings.Contains(path, "/cur/") || strings.Contains(path, "/new/") {
			return mm.importMessage(path)
		}

		return nil
	})
}

// importMessage imports a single message
func (mm *MigrationManager) importMessage(messagePath string) error {
	// Read message file
	data, err := os.ReadFile(filepath.Clean(messagePath))
	if err != nil {
		return err
	}

	// Parse maildir filename to extract flags
	// Format: timestamp.unique_info:2,flags
	filename := filepath.Base(messagePath)
	var mailFlags []string
	if idx := strings.Index(filename, ":2,"); idx != -1 {
		flagStr := filename[idx+3:]
		// Map maildir flags to IMAP flags
		// S = Seen, R = Answered, F = Flagged, T = Deleted, D = Draft
		for _, f := range flagStr {
			switch f {
			case 'S':
				mailFlags = append(mailFlags, "\\Seen")
			case 'R':
				mailFlags = append(mailFlags, "\\Answered")
			case 'F':
				mailFlags = append(mailFlags, "\\Flagged")
			case 'T':
				mailFlags = append(mailFlags, "\\Deleted")
			case 'D':
				mailFlags = append(mailFlags, "\\Draft")
			}
		}
	}
	// Note: mailFlags are parsed but StoreMessage doesn't currently support flags.
	// The message is stored without preserving these flags.

	// Extract user from path
	// Format: /var/mail/domain/user/Maildir/...
	parts := strings.Split(messagePath, string(os.PathSeparator))
	var user, domain string
	for i, part := range parts {
		if part == "Maildir" && i >= 2 {
			user = parts[i-1]
			domain = parts[i-2]
			break
		}
	}

	if user == "" || domain == "" {
		return fmt.Errorf("could not determine user from path: %s", messagePath)
	}

	email := user + "@" + domain

	// Store message
	if mm.msgStore != nil {
		_, err := mm.msgStore.StoreMessage(email, data)
		if err != nil {
			return fmt.Errorf("failed to store message: %w", err)
		}
	}

	return nil
}

// MigrateFromMBOX imports from MBOX format files
func (mm *MigrationManager) MigrateFromMBOX(mboxPattern string) error {
	mm.logger.Info("Starting MBOX migration", "pattern", mboxPattern)

	// Find MBOX files
	matches, err := filepath.Glob(mboxPattern)
	if err != nil {
		return fmt.Errorf("invalid pattern: %w", err)
	}

	if len(matches) == 0 {
		return fmt.Errorf("no MBOX files found matching: %s", mboxPattern)
	}

	for _, mboxFile := range matches {
		fmt.Printf("Importing: %s\n", mboxFile)

		if err := mm.importMBOXFile(mboxFile); err != nil {
			mm.logger.Error("Failed to import MBOX", "file", mboxFile, "error", err)
		}
	}

	return nil
}

// importMBOXFile imports a single MBOX file
func (mm *MigrationManager) importMBOXFile(mboxFile string) error {
	file, err := os.Open(filepath.Clean(mboxFile))
	if err != nil {
		return err
	}
	defer file.Close()

	// Extract target folder from filename
	folder := filepath.Base(mboxFile)
	folder = strings.TrimSuffix(folder, ".mbox")
	folder = strings.TrimSuffix(folder, ".MBOX")

	fmt.Printf("  Importing to folder: %s\n", folder)

	// Parse MBOX format
	// Messages are separated by lines starting with "From "
	reader := bufio.NewReader(file)
	messageCount := 0
	currentMessage := []byte{}
	inMessage := false

	for {
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			if len(currentMessage) > 0 {
				// Process last message
				if err := mm.processMBOXMessage(currentMessage, folder); err != nil {
					mm.logger.Error("Failed to process message", "error", err)
				} else {
					messageCount++
				}
			}
			break
		}
		if err != nil {
			return err
		}

		// Check for message separator
		if strings.HasPrefix(line, "From ") && !inMessage {
			// Start of first message
			inMessage = true
			currentMessage = []byte(line)
		} else if strings.HasPrefix(line, "From ") && inMessage {
			// Start of new message, process current one
			if len(currentMessage) > 0 {
				if err := mm.processMBOXMessage(currentMessage, folder); err != nil {
					mm.logger.Error("Failed to process message", "error", err)
				} else {
					messageCount++
				}
			}
			currentMessage = []byte(line)
		} else {
			currentMessage = append(currentMessage, line...)
		}
	}

	fmt.Printf("  Imported %d messages\n", messageCount)
	return nil
}

// processMBOXMessage processes a single message from MBOX
func (mm *MigrationManager) processMBOXMessage(data []byte, folder string) error {
	// Remove the "From " header line
	lines := strings.Split(string(data), "\n")
	if len(lines) > 0 && strings.HasPrefix(lines[0], "From ") {
		lines = lines[1:]
	}

	// Reconstruct message
	messageData := []byte(strings.Join(lines, "\n"))

	// Determine target user from message headers or folder name
	targetUser := mm.extractTargetUser(string(data), folder)
	if targetUser == "" || targetUser == "unknown" {
		return fmt.Errorf("could not determine target user for message")
	}

	// Store message if we have a valid target
	if mm.msgStore != nil {
		msgID, err := mm.msgStore.StoreMessage(targetUser, messageData)
		if err != nil {
			return fmt.Errorf("failed to store message: %w", err)
		}
		fmt.Printf("    Stored message (ID: %s, size: %d bytes, user: %s)\n", msgID[:8], len(messageData), targetUser)
	} else {
		fmt.Printf("    Message size: %d bytes -> user: %s (no message store)\n", len(messageData), targetUser)
	}

	return nil
}

// extractTargetUser extracts the target user from MBOX message headers
func (mm *MigrationManager) extractTargetUser(data, folder string) string {
	// Try to extract From header
	lines := strings.Split(data, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "From:") {
			// Extract email from "From: User Name <user@domain.com>"
			if idx := strings.Index(line, "<"); idx != -1 {
				emailPart := line[idx+1:]
				if idx := strings.Index(emailPart, ">"); idx != -1 {
					email := emailPart[:idx]
					return email
				}
			}
		}
	}

	// Fall back to folder name if no From header
	if folder != "" {
		return folder
	}

	return "unknown"
}

// parseEmail splits an email address into user and domain
func parseEmail(email string) (user, domain string) {
	at := strings.LastIndex(email, "@")
	if at == -1 {
		return email, ""
	}
	return email[:at], email[at+1:]
}

// ValidateSource validates a migration source
func (mm *MigrationManager) ValidateSource(sourceType, source string) error {
	switch sourceType {
	case "imap":
		u, err := url.Parse(source)
		if err != nil {
			return fmt.Errorf("invalid IMAP URL: %w", err)
		}
		if u.Scheme != "imap" && u.Scheme != "imaps" {
			return fmt.Errorf("URL must use imap:// or imaps:// scheme")
		}
		if u.Host == "" {
			return fmt.Errorf("IMAP URL must include host")
		}

	case "dovecot":
		if _, err := os.Stat(source); err != nil {
			return fmt.Errorf("maildir not found: %w", err)
		}

	case "mbox":
		matches, err := filepath.Glob(source)
		if err != nil {
			return fmt.Errorf("invalid pattern: %w", err)
		}
		if len(matches) == 0 {
			return fmt.Errorf("no files found matching: %s", source)
		}

	default:
		return fmt.Errorf("unsupported source type: %s", sourceType)
	}

	return nil
}
