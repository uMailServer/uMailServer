// Package backup provides backup and restore functionality for uMailServer
package backup

import (
	"archive/tar"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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

// BackupUser creates a backup of a specific user's data
func (m *Manager) BackupUser(user string, destPath string, opts BackupOptions) error {
	userPath := filepath.Join(m.dataDir, "messages", user)
	if _, err := os.Stat(userPath); os.IsNotExist(err) {
		return fmt.Errorf("user %s does not exist", user)
	}

	return m.backupUserToPath(user, destPath, opts)
}

// backupUserToPath creates a tar.gz archive of a user's maildir
func (m *Manager) backupUserToPath(user, destPath string, opts BackupOptions) error {
	userPath := filepath.Join(m.dataDir, "messages", user)

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create backup file: %w", err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()

	tw := tar.NewWriter(gz)
	defer tw.Close()

	return m.addDirToTar(userPath, user, tw)
}

// addDirToTar recursively adds a directory to a tar archive
// basePath is the directory to walk, relPath is the archive prefix for all entries
func (m *Manager) addDirToTar(basePath, relPath string, tw *tar.Writer) error {
	return filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}

		// Compute archive path: relPath prefix + relative path from basePath
		rel, err := filepath.Rel(basePath, path)
		if err != nil {
			return err
		}

		arcPath := filepath.Join(relPath, rel)
		if info.Mode().IsDir() && arcPath != "" && !strings.HasSuffix(arcPath, "/") {
			arcPath += "/"
		}
		header.Name = arcPath

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if !info.Mode().IsDir() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()
			if _, err := io.Copy(tw, file); err != nil {
				return err
			}
		}
		return nil
	})
}

// BackupMailbox creates a backup of a specific mailbox
func (m *Manager) BackupMailbox(user, mailbox, destPath string, opts BackupOptions) error {
	mailboxPath := filepath.Join(m.dataDir, "messages", user, mailbox)
	if _, err := os.Stat(mailboxPath); os.IsNotExist(err) {
		return fmt.Errorf("mailbox %s for user %s does not exist", mailbox, user)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create backup file: %w", err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()

	tw := tar.NewWriter(gz)
	defer tw.Close()

	return m.addDirToTar(mailboxPath, "", tw)
}

// BackupFull creates a full system backup
func (m *Manager) BackupFull(destPath string, opts BackupOptions) error {
	messagesDir := filepath.Join(m.dataDir, "messages")

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create backup file: %w", err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()

	tw := tar.NewWriter(gz)
	defer tw.Close()

	return m.addDirToTar(messagesDir, "messages", tw)
}

// ListUserBackups returns available backups for a specific user
func (m *Manager) ListUserBackups(user string) ([]BackupInfo, error) {
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

	// Try to read as tar
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("invalid gzip format: %w", err)
	}
	gz.Close()

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
	if !m.CanRestore(backupPath) {
		return fmt.Errorf("cannot restore: invalid backup file")
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
		targetDir = filepath.Join(m.dataDir, "messages", opts.TargetUser)
	case RestoreModeMerge:
		targetDir = filepath.Join(m.dataDir, "messages")
	default:
		targetDir = filepath.Join(m.dataDir, "messages")
	}

	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		targetPath := filepath.Join(targetDir, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			parentDir := filepath.Dir(targetPath)
			if err := os.MkdirAll(parentDir, 0755); err != nil {
				return err
			}

			if !opts.Overwrite {
				if _, exists := os.Stat(targetPath); exists == nil {
					return fmt.Errorf("file already exists: %s", targetPath)
				}
			}

			outFile, err := os.Create(targetPath)
			if err != nil {
				return err
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()
			os.Chmod(targetPath, os.FileMode(header.Mode))
		}
	}

	return nil
}

// Encrypt encrypts a file using AES-GCM
func (m *Manager) Encrypt(srcPath, destPath, password string) error {
	key := sha256.Sum256([]byte(password))

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}

	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}

	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write(salt); err != nil {
		return err
	}
	if _, err := f.Write(nonce); err != nil {
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

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	if _, err := f.Write(ciphertext); err != nil {
		return err
	}

	return nil
}

// Decrypt decrypts an AES-GCM encrypted file
func (m *Manager) Decrypt(srcPath, destPath, password string) error {
	key := sha256.Sum256([]byte(password))

	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()

	salt := make([]byte, 16)
	if _, err := f.Read(salt); err != nil {
		return err
	}

	nonce := make([]byte, 16)
	if _, err := f.Read(nonce); err != nil {
		return err
	}

	ciphertext, err := io.ReadAll(f)
	if err != nil {
		return err
	}

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return fmt.Errorf("decryption failed (wrong password?): %w", err)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := out.Write(plaintext); err != nil {
		return err
	}

	return nil
}
