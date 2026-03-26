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

	fmt.Printf("Host: %s\n", u.Host)
	fmt.Printf("Username: %s\n", opts.Username)

	// TODO: Implement actual IMAP sync using go-imap client
	// For now, this is a placeholder implementation

	fmt.Println("IMAP migration would:")
	fmt.Println("1. Connect to source IMAP server")
	fmt.Println("2. List mailboxes/folders")
	fmt.Println("3. Sync messages from each folder")
	fmt.Println("4. Preserve flags and metadata")

	return fmt.Errorf("IMAP migration not yet fully implemented")
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

	file, err := os.Open(passwdFile)
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
	data, err := os.ReadFile(messagePath)
	if err != nil {
		return err
	}

	// Parse maildir filename to extract flags
	// Format: timestamp.unique_info:2,flags
	filename := filepath.Base(messagePath)
	_ = filename // TODO: use filename
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
	_ = mailFlags // TODO: use mailFlags

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
	file, err := os.Open(mboxFile)
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
	messageData := strings.Join(lines, "\n")

	// TODO: Determine target user from message headers or folder
	// For now, this is a placeholder

	fmt.Printf("    Message size: %d bytes -> folder: %s\n", len(messageData), folder)

	return nil
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
