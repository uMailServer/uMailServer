// Package backup provides backup and restore functionality for uMailServer
package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/pbkdf2"

	"github.com/umailserver/umailserver/internal/storage"
)

// Manager handles backup and restore operations with support for per-user backups
type Manager struct {
	dataDir  string
	db       *storage.Database
	msgStore *storage.MessageStore
}

// NewManager creates a new backup manager
func NewManager(dataDir string, db *storage.Database, msgStore *storage.MessageStore) *Manager {
	return &Manager{
		dataDir:  dataDir,
		db:       db,
		msgStore: msgStore,
	}
}

// messagesDir returns the message store root. The server builds Manager with
// cfg.Server.DataDir and keeps its message store at <dataDir>/mail/messages
// (server.New), so that tree is used whenever it exists; <dataDir>/messages is
// the legacy layout. Using only the legacy path made every backup of a live
// server fail or come out empty and restored mail where the store never
// reads it (F5552).
func (m *Manager) messagesDir() string {
	live := filepath.Join(m.dataDir, "mail", "messages")
	if info, err := os.Stat(live); err == nil && info.IsDir() {
		return live
	}
	return filepath.Join(m.dataDir, "messages")
}

// validatePathPart rejects empty names, "..", and any path separator in
// caller-supplied user/mailbox identifiers, matching
// storage.MessageStore.validatePathComponent. These values are joined into
// filesystem paths, so without this check "../victim" reads or writes
// another user's mail.
func validatePathPart(part string) error {
	// "." is rejected like storage.validatePathComponent does (F5721): it
	// resolves to the message store root, so BackupUser(".") archived every
	// user's mail and a "." restore target wrote into the store root (F7540).
	if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "/\\") {
		return fmt.Errorf("invalid path component: %q", part)
	}
	return nil
}

// BackupUser creates a backup of a specific user's data
func (m *Manager) BackupUser(user string, destPath string, opts BackupOptions) error {
	if err := validatePathPart(user); err != nil {
		return fmt.Errorf("invalid user: %w", err)
	}

	userPath := filepath.Join(m.messagesDir(), user)
	if _, err := os.Stat(userPath); os.IsNotExist(err) {
		return fmt.Errorf("user %s does not exist", user)
	}

	return m.backupUserToPath(user, destPath, opts)
}

// backupUserToPath creates a tar.gz archive of a user's maildir
func (m *Manager) backupUserToPath(user, destPath string, opts BackupOptions) (retErr error) {
	userPath := filepath.Join(m.messagesDir(), user)

	if err := os.MkdirAll(filepath.Dir(destPath), 0o750); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	f, err := createPrivate(destPath)
	if err != nil {
		return fmt.Errorf("failed to create backup file: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, f.Close()) }()

	gz := gzip.NewWriter(f)
	defer func() { retErr = errors.Join(retErr, gz.Close()) }()

	tw := tar.NewWriter(gz)
	defer func() { retErr = errors.Join(retErr, tw.Close()) }()

	return m.addDirToTar(userPath, user, tw)
}

// archiveFileHook, when set, runs after a file was listed by the walk and
// before it is opened. Tests use it to model Maildir deliveries and flag
// renames racing with a backup deterministically (F6960).
var archiveFileHook func(path string)

// addDirToTar recursively adds a directory to a tar archive
// basePath is the directory to walk, relPath is the archive prefix for all entries
func (m *Manager) addDirToTar(basePath, relPath string, tw *tar.Writer) error {
	return filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// A live Maildir loses entries between the directory listing and
			// the stat (delivery moves tmp/ to new/, IMAP renames for flags).
			// Skip what vanished; the root itself must exist (F6960).
			if os.IsNotExist(err) && path != basePath {
				return nil
			}
			return err
		}

		// Only directories and regular files are archived. A symlink header
		// carries Size 0 but the copy below follows the link, so archiving one
		// failed with tar.ErrWriteTooLong (or pulled in the target's content);
		// a FIFO would block the backup forever on open (F5912).
		if !info.Mode().IsDir() && !info.Mode().IsRegular() {
			return nil
		}

		// Compute archive path: relPath prefix + relative path from basePath
		rel, err := filepath.Rel(basePath, path)
		if err != nil {
			return err
		}

		arcPath := filepath.Join(relPath, rel)
		if info.Mode().IsDir() {
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			if arcPath != "" && !strings.HasSuffix(arcPath, "/") {
				arcPath += "/"
			}
			header.Name = arcPath
			return tw.WriteHeader(header)
		}

		return addFileToTar(tw, path, arcPath)
	})
}

// addFileToTar archives one regular file. The header is built from the opened
// file (fstat) rather than the stale walk listing, so a file that changed in
// between is archived consistently, and a file that disappeared is skipped
// (F6960).
func addFileToTar(tw *tar.Writer, path, arcPath string) error {
	if archiveFileHook != nil {
		archiveFileHook(path)
	}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	header.Name = arcPath
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	// Copy exactly the announced size: a file appended to while being read
	// must not overrun the header (tar.ErrWriteTooLong).
	n, err := io.Copy(tw, io.LimitReader(file, header.Size))
	if err != nil {
		return err
	}
	if n != header.Size {
		return fmt.Errorf("file %s shrank while being archived", path)
	}
	return nil
}

// BackupMailbox creates a backup of a specific mailbox
func (m *Manager) BackupMailbox(user, mailbox, destPath string, opts BackupOptions) (retErr error) {
	if err := validatePathPart(user); err != nil {
		return fmt.Errorf("invalid user: %w", err)
	}
	if err := validatePathPart(mailbox); err != nil {
		return fmt.Errorf("invalid mailbox: %w", err)
	}

	mailboxPath := filepath.Join(m.messagesDir(), user, mailbox)
	if _, err := os.Stat(mailboxPath); os.IsNotExist(err) {
		return fmt.Errorf("mailbox %s for user %s does not exist", mailbox, user)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o750); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	f, err := createPrivate(destPath)
	if err != nil {
		return fmt.Errorf("failed to create backup file: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, f.Close()) }()

	gz := gzip.NewWriter(f)
	defer func() { retErr = errors.Join(retErr, gz.Close()) }()

	tw := tar.NewWriter(gz)
	defer func() { retErr = errors.Join(retErr, tw.Close()) }()

	return m.addDirToTar(mailboxPath, "", tw)
}

// BackupFull creates a full system backup
func (m *Manager) BackupFull(destPath string, opts BackupOptions) (retErr error) {
	messagesDir := m.messagesDir()

	if err := os.MkdirAll(filepath.Dir(destPath), 0o750); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	f, err := createPrivate(destPath)
	if err != nil {
		return fmt.Errorf("failed to create backup file: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, f.Close()) }()

	gz := gzip.NewWriter(f)
	defer func() { retErr = errors.Join(retErr, gz.Close()) }()

	tw := tar.NewWriter(gz)
	defer func() { retErr = errors.Join(retErr, tw.Close()) }()

	// Entries are rooted at messages/ ("alice/...") like BackupUser archives,
	// because Restore extracts under <dataDir>/messages. A "messages" prefix
	// restored everything into messages/messages/<user> (F5351).
	return m.addDirToTar(messagesDir, "", tw)
}

// createPrivate creates or truncates path with owner-only permissions. Backup
// archives and decrypted output hold users' mail, so they get the 0600 mode
// the maildir and the CLI backup writer use; os.Create left them 0666 minus
// umask, readable by other local users (F5352). Chmod covers a pre-existing
// file, whose mode OpenFile does not change.
func createPrivate(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	if err := f.Chmod(0o600); err != nil {
		return nil, errors.Join(err, f.Close())
	}
	return f, nil
}

// ListUserBackups returns available backups for a specific user
func (m *Manager) ListUserBackups(user string) ([]BackupInfo, error) {
	if err := validatePathPart(user); err != nil {
		return nil, fmt.Errorf("invalid user: %w", err)
	}

	backupDir := filepath.Join(m.dataDir, "backups", "per-user", user)

	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		return nil, nil
	}

	return listBackupsInDir(backupDir)
}

// listBackupsInDir returns all backups in a directory
func listBackupsInDir(dir string) ([]BackupInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var backups []BackupInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		ext := filepath.Ext(name)
		if ext != ".gz" && ext != ".enc" {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		backups = append(backups, BackupInfo{
			Filename: name,
			Size:     info.Size(),
			ModTime:  info.ModTime(),
			Path:     filepath.Join(dir, name),
		})
	}

	return backups, nil
}

// CanRestore returns true if restore can be performed
func (m *Manager) CanRestore(backupPath string) bool {
	info, err := os.Stat(backupPath)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return false
	}
	return true
}

// GetBackupInfo returns information about a backup file
func (m *Manager) GetBackupInfo(backupPath string) (*BackupManifest, error) {
	info, err := os.Stat(backupPath)
	if err != nil {
		return nil, err
	}

	manifest := &BackupManifest{
		Filename:  filepath.Base(backupPath),
		Size:      info.Size(),
		CreatedAt: info.ModTime(),
		Path:      backupPath,
	}

	return manifest, nil
}

// Verify checks backup file integrity
func (m *Manager) Verify(backupPath string) (*BackupManifest, error) {
	info, err := os.Stat(backupPath)
	if err != nil {
		return nil, fmt.Errorf("backup file not found: %w", err)
	}

	manifest := &BackupManifest{
		Filename:  filepath.Base(backupPath),
		Size:      info.Size(),
		CreatedAt: info.ModTime(),
		Path:      backupPath,
	}

	f, err := os.Open(backupPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Check for gzip magic bytes
	buf := make([]byte, 2)
	if _, err := f.Read(buf); err != nil {
		return nil, err
	}
	if buf[0] != 0x1f || buf[1] != 0x8b {
		return nil, fmt.Errorf("not a valid gzip file")
	}

	// Verify the archive structurally: the gzip stream must decompress
	// completely and the payload must be a well-formed tar with at least one
	// entry (the format backupUserToPath writes). Previously only the
	// 10-byte gzip header was validated, so truncated streams and gzip files
	// that are not backups at all verified as intact.
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("invalid gzip format: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	entries := 0
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("corrupt archive: %w", err)
		}
		entries++
		// Consume each entry payload so corruption inside file data is
		// detected too.
		if _, err := io.Copy(io.Discard, tr); err != nil {
			return nil, fmt.Errorf("corrupt archive entry %s: %w", header.Name, err)
		}
	}
	if entries == 0 {
		return nil, fmt.Errorf("corrupt archive: no entries")
	}

	// Tar EOF can precede gzip EOF; finish reading to validate the gzip footer.
	if _, err := io.Copy(io.Discard, gz); err != nil {
		return nil, fmt.Errorf("corrupt gzip stream: %w", err)
	}

	// Compute checksum
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	manifest.Checksum = base64.StdEncoding.EncodeToString(h.Sum(nil))

	return manifest, nil
}

// Restore restores a backup to the specified location
func (m *Manager) Restore(backupPath string, opts RestoreOptions) error {
	if opts.VerifyOnly {
		_, err := m.Verify(backupPath)
		return err
	}

	if !m.CanRestore(backupPath) {
		return fmt.Errorf("cannot restore: invalid backup file")
	}

	// Validate the whole archive before extracting anything. Extraction
	// renames members into the live tree one by one, so a truncated or
	// corrupt archive otherwise leaves a half-restored mailbox behind before
	// the damage is noticed at the end of the stream (F5914).
	if _, err := m.Verify(backupPath); err != nil {
		return fmt.Errorf("refusing to restore: %w", err)
	}

	f, err := os.Open(backupPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("invalid gzip format: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)

	var targetDir string
	switch opts.Mode {
	case RestoreModeDifferent:
		if err := validatePathPart(opts.TargetUser); err != nil {
			return fmt.Errorf("invalid target user: %w", err)
		}
		targetDir = filepath.Join(m.messagesDir(), opts.TargetUser)
	case RestoreModeMerge:
		targetDir = m.messagesDir()
	default:
		targetDir = m.messagesDir()
	}

	// Resolve every entry before extracting any: renaming members into the live
	// tree one by one otherwise leaves a partially restored mailbox when a later
	// entry names another user or escapes the target (F7541).
	if err := preflightEntries(backupPath, opts.Mode, targetDir); err != nil {
		return err
	}

	if err := os.MkdirAll(targetDir, 0o750); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	var sourceUser string
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		targetPath, err := resolveEntry(header.Name, opts.Mode, targetDir, &sourceUser)
		if err != nil {
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			// Keep the owner able to enter and fill the directory: a
			// header mode without rwx for the owner (0, 0555) made every
			// member below it fail to extract (F5911).
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode).Perm()|0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			parentDir := filepath.Dir(targetPath)
			if err := os.MkdirAll(parentDir, 0o750); err != nil {
				return err
			}

			if !opts.Overwrite {
				if _, exists := os.Stat(targetPath); exists == nil {
					return fmt.Errorf("file already exists: %s", targetPath)
				}
			}

			if err := restoreRegularFile(targetPath, tr, os.FileMode(header.Mode)); err != nil {
				return err
			}
		}
	}

	// Tar EOF can precede gzip EOF; the gzip CRC-32/size footer is only
	// checked when the stream is read to its end, so drain it. Without this a
	// payload corrupted in a way that keeps the deflate framing valid restores
	// silently, even though Verify rejects the same archive.
	if _, err := io.Copy(io.Discard, gz); err != nil {
		return fmt.Errorf("corrupt gzip stream: %w", err)
	}

	return nil
}

// resolveEntry maps an archive member name to its extraction path, rejecting
// names that belong to another user (different-user restore) or resolve outside
// targetDir. A tar entry must never leave the target directory: "../escape.txt"
// would let a crafted archive overwrite arbitrary files. The CLI restore
// (internal/cli/backup.go) enforces the same rule.
func resolveEntry(name string, mode RestoreMode, targetDir string, sourceUser *string) (string, error) {
	orig := name
	if mode == RestoreModeDifferent {
		var err error
		name, err = relocateEntry(name, sourceUser)
		if err != nil {
			return "", err
		}
	}
	targetPath := filepath.Join(targetDir, name)
	absTargetPath, err := filepath.Abs(targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve target path: %w", err)
	}
	absTargetDir, err := filepath.Abs(targetDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve target directory: %w", err)
	}
	absTargetPath = filepath.Clean(absTargetPath)
	absTargetDir = filepath.Clean(absTargetDir)
	if absTargetPath != absTargetDir &&
		!strings.HasPrefix(absTargetPath, absTargetDir+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid entry in backup: %s - would extract outside target directory", orig)
	}
	return targetPath, nil
}

// preflightEntries reads the archive once and resolves every member name
// without touching the filesystem (F7541).
func preflightEntries(backupPath string, mode RestoreMode, targetDir string) error {
	f, err := os.Open(backupPath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("invalid gzip format: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var sourceUser string
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := resolveEntry(header.Name, mode, targetDir, &sourceUser); err != nil {
			return err
		}
	}
}

// relocateEntry strips the source user from a per-user archive entry
// ("alice/cur/msg1" -> "cur/msg1") so RestoreModeDifferent extracts under
// messages/<TargetUser>/ instead of messages/<TargetUser>/alice/ (F5350).
// Every entry must name the same source user; archives holding several users
// or no user directory (full or mailbox backups) are rejected rather than
// merged into the target user.
func relocateEntry(name string, sourceUser *string) (string, error) {
	user, rest, _ := strings.Cut(path.Clean(name), "/")
	if user == "." || validatePathPart(user) != nil || (*sourceUser != "" && user != *sourceUser) {
		return "", fmt.Errorf("invalid entry for different-user restore: %s - archive is not a single-user backup", name)
	}
	*sourceUser = user
	return rest, nil
}

// restoreRegularFile writes one archive member to targetPath atomically: the
// payload goes to a temporary file in the same directory and is renamed over
// targetPath only after it was copied completely. Creating targetPath directly
// truncated an existing message before the payload was read, so a truncated or
// corrupt archive destroyed the live copy it was meant to restore.
func restoreRegularFile(targetPath string, r io.Reader, mode os.FileMode) (retErr error) {
	tmp, err := os.CreateTemp(filepath.Dir(targetPath), ".restore-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if retErr != nil {
			// Best-effort cleanup of our own temp file; the restore error is
			// what the caller needs to see.
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := io.Copy(tmp, r); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, mode.Perm()); err != nil {
		return err
	}
	return os.Rename(tmpName, targetPath)
}

// backupKDFIterations is the PBKDF2-HMAC-SHA256 iteration count for backup
// envelope encryption (OWASP guidance for PBKDF2-HMAC-SHA256). Backup
// encryption is a rare, user-initiated operation, so the ~0.5s derive cost is
// acceptable for the precomputation resistance it buys.
const backupKDFIterations = 600000

// backupEnvelopeMagic prefixes v2 envelopes: "BK" + version byte.
var backupEnvelopeMagic = []byte{'B', 'K'}

const backupEnvelopeVersion2 = byte(0x02)

// Encrypt encrypts a file using AES-256-GCM with a per-file salt. The key is
// derived with PBKDF2-HMAC-SHA256 over the password and a fresh 16-byte salt,
// so two backups sharing a password never share a key. Envelope format v2:
// "BK" | 0x02 | salt(16) | nonce | ciphertext. Files written by the previous
// format (salt | nonce | ciphertext, unsalted SHA-256 key) remain decryptable
// via Decrypt's legacy path.
func (m *Manager) Encrypt(srcPath, destPath, password string) (retErr error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}

	key := pbkdf2.Key([]byte(password), salt, backupKDFIterations, 32, sha256.New)

	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}

	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	plaintext, err := io.ReadAll(src)
	if err != nil {
		return err
	}

	f, err := createPrivate(destPath)
	if err != nil {
		return err
	}
	// A deferred Close that drops its error hides a failed final flush (full
	// disk, quota, NFS), leaving a truncated envelope reported as success (F5910).
	defer func() { retErr = errors.Join(retErr, f.Close()) }()

	if _, err := f.Write(backupEnvelopeMagic); err != nil {
		return err
	}
	if _, err := f.Write([]byte{backupEnvelopeVersion2}); err != nil {
		return err
	}
	if _, err := f.Write(salt); err != nil {
		return err
	}
	if _, err := f.Write(nonce); err != nil {
		return err
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	if _, err := f.Write(ciphertext); err != nil {
		return err
	}

	return nil
}

// Decrypt decrypts an AES-GCM encrypted file. It auto-detects the envelope
// version: v2 files (prefixed "BK" + 0x02) derive their key with PBKDF2 over
// the stored per-file salt; legacy files (salt | nonce | ciphertext) keep the
// historical unsalted SHA-256(password) derivation so old backups stay
// recoverable.
func (m *Manager) Decrypt(srcPath, destPath, password string) (retErr error) {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()

	// Peek the envelope magic. Legacy v1 files begin with the 16-byte salt,
	// which is random; a legacy salt whose first two bytes happen to be "BK"
	// (probability 1/65536) is rejected by GCM authentication and fails
	// closed, so mis-detection cannot produce corrupted plaintext.
	magic := make([]byte, len(backupEnvelopeMagic))
	if _, err := io.ReadFull(f, magic); err != nil {
		return err
	}

	var key []byte
	var gcm cipher.AEAD

	if bytes.Equal(magic, backupEnvelopeMagic) {
		versionBuf := make([]byte, 1)
		if _, err := io.ReadFull(f, versionBuf); err != nil {
			return err
		}
		version := versionBuf[0]
		if version != backupEnvelopeVersion2 {
			return fmt.Errorf("unsupported backup envelope version %d", version)
		}
		salt := make([]byte, 16)
		if _, err := io.ReadFull(f, salt); err != nil {
			return err
		}
		key = pbkdf2.Key([]byte(password), salt, backupKDFIterations, 32, sha256.New)

		block, err := aes.NewCipher(key)
		if err != nil {
			return err
		}
		gcm, err = cipher.NewGCM(block)
		if err != nil {
			return err
		}
	} else {
		// Legacy v1 framing: salt(16) [unused] | nonce | ciphertext, with the
		// historical unsalted SHA-256(password) key.
		//
		// DESIGN DECISION (2026-10-02): the v1 read path is kept so backups
		// encrypted before the salted v2 envelope shipped remain recoverable;
		// removing it would orphan existing backups with no security gain.
		// The path is DEPRECATED: every use is logged so operators know to
		// migrate (decrypt, then re-encrypt with Encrypt), and v1 support is
		// scheduled for removal in a future major version after the warning
		// window. Encrypt has only ever written v2 since the change.
		slog.Warn("deprecated legacy v1 backup envelope: unsalted key derivation",
			"src", srcPath,
			"migration", "decrypt with Decrypt, then re-encrypt with Encrypt to migrate to the v2 envelope",
		)

		legacySalt := make([]byte, 16)
		copy(legacySalt, magic)
		if _, err := io.ReadFull(f, legacySalt[len(magic):]); err != nil {
			return err
		}
		legacyKey := sha256.Sum256([]byte(password))

		block, err := aes.NewCipher(legacyKey[:])
		if err != nil {
			return err
		}
		gcm, err = cipher.NewGCM(block)
		if err != nil {
			return err
		}
	}

	// Encrypt writes gcm.NonceSize() bytes of nonce, so Decrypt must read
	// exactly that many. A hardcoded length desynchronises the ciphertext and
	// makes every encrypted backup unrecoverable.
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(f, nonce); err != nil {
		return err
	}

	ciphertext, err := io.ReadAll(f)
	if err != nil {
		return err
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return fmt.Errorf("decryption failed (wrong password?): %w", err)
	}

	out, err := createPrivate(destPath)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, out.Close()) }()

	if _, err := out.Write(plaintext); err != nil {
		return err
	}

	return nil
}
