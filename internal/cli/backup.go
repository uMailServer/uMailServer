package cli

import (
	"archive/tar"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
	bolterrors "go.etcd.io/bbolt/errors"
	"golang.org/x/crypto/scrypt"

	"github.com/umailserver/umailserver/internal/config"
)

// Backup encryption constants
const (
	backupMagic   = "UMAILBACKUP"
	backupVersion = 1
	saltSize      = 32
	nonceSize     = 12
	keySize       = 32 // AES-256
)

// backupLockTimeout bounds the wait for the bbolt file lock (F5551).
const backupLockTimeout = time.Second

// fileHash tracks a file's hash for integrity verification
type fileHash struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

// BackupManager handles backup and restore operations
type BackupManager struct {
	config   *config.Config
	hashes   []fileHash
	password string
}

// NewBackupManager creates a new backup manager
func NewBackupManager(cfg *config.Config) *BackupManager {
	return &BackupManager{
		config: cfg,
	}
}

// SetPassword sets the encryption password for backups
func (bm *BackupManager) SetPassword(password string) {
	bm.password = password
}

// Backup creates a full backup of the server
func (bm *BackupManager) Backup(backupPath string) error {
	// Reset hashes for this backup
	bm.hashes = []fileHash{}

	timestamp := time.Now().Format("20060102_150405")
	extension := ".tar.gz"
	if bm.password != "" {
		extension = ".tar.gz.enc"
	}
	backupFile := filepath.Join(backupPath, fmt.Sprintf("umailserver_backup_%s%s", timestamp, extension))

	// Hold the bbolt shared lock on the databases for the whole backup so no
	// writer can commit while they are copied (F5551).
	release, err := bm.lockDatabases()
	if err != nil {
		return err
	}
	defer release()

	// Create backup directory
	if err := os.MkdirAll(backupPath, 0o750); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Create tar.gz in memory first
	fmt.Printf("Creating backup archive...\n")
	var tarData []byte
	{
		// We'll write to a bytes.Buffer, then compress, then encrypt
		tarBuffer := new(strings.Builder)
		gw := gzip.NewWriter(tarBuffer)
		tw := tar.NewWriter(gw)

		// Backup config
		fmt.Println("  Adding configuration...")
		if err := bm.backupConfig(tw); err != nil {
			return fmt.Errorf("failed to backup config: %w", err)
		}

		// Backup database
		fmt.Println("  Adding database...")
		if err := bm.backupDatabase(tw); err != nil {
			return fmt.Errorf("failed to backup database: %w", err)
		}

		// Backup maildir
		fmt.Println("  Adding maildir...")
		if err := bm.backupMaildir(tw); err != nil {
			return fmt.Errorf("failed to backup maildir: %w", err)
		}

		// Create backup manifest
		fmt.Println("  Adding manifest...")
		if err := bm.createManifest(tw, timestamp); err != nil {
			return fmt.Errorf("failed to create manifest: %w", err)
		}

		if err := tw.Close(); err != nil {
			return fmt.Errorf("failed to close tar writer: %w", err)
		}
		if err := gw.Close(); err != nil {
			return fmt.Errorf("failed to close gzip writer: %w", err)
		}

		tarData = []byte(tarBuffer.String())
	}

	// Write to file (optionally encrypted)
	if bm.password != "" {
		fmt.Printf("Encrypting backup with AES-256-GCM...\n")
		encrypted, err := bm.encryptBackup(tarData)
		if err != nil {
			return fmt.Errorf("failed to encrypt backup: %w", err)
		}
		if err := writeNewFile(backupFile, encrypted); err != nil {
			return fmt.Errorf("failed to write encrypted backup: %w", err)
		}
	} else {
		// Warn about unencrypted backup
		fmt.Printf("WARNING: Backup is NOT ENCRYPTED. Sensitive data may be exposed.\n")
		fmt.Printf("         Use SetPassword() to enable AES-256-GCM encryption.\n")

		if err := writeNewFile(backupFile, tarData); err != nil {
			return fmt.Errorf("failed to write backup: %w", err)
		}
	}

	fmt.Printf("Backup completed successfully: %s\n", backupFile)
	return nil
}

// lockDatabases opens the bbolt databases the server keeps under DataDir
// (umailserver.db and mail/mail.db, see server.New) read-only, which takes
// bbolt's shared file lock, and keeps them open until release is called. A
// running server holds both files under bbolt's exclusive lock and commits to
// them at any time; a plain sequential copy taken meanwhile can pair an old
// meta page with pages a later commit reused, so the archived database
// silently holds wrong data. Backup therefore refuses while the lock is held
// by another process. A file that is not a bbolt database is archived
// byte-for-byte as before.
func (bm *BackupManager) lockDatabases() (release func(), err error) {
	var held []*bolt.DB
	release = func() {
		for _, d := range held {
			// Read-only handle: nothing to flush; Close only drops the lock.
			_ = d.Close()
		}
	}
	for _, p := range []string{
		filepath.Join(bm.config.Server.DataDir, "umailserver.db"),
		filepath.Join(bm.config.Server.DataDir, "mail", "mail.db"),
	} {
		if info, statErr := os.Stat(p); statErr != nil || !info.Mode().IsRegular() {
			continue // absent or not a regular file: handled by the copy step
		}
		d, openErr := bolt.Open(p, 0o600, &bolt.Options{ReadOnly: true, Timeout: backupLockTimeout})
		if errors.Is(openErr, bolterrors.ErrTimeout) {
			release()
			return nil, fmt.Errorf("database %s is locked by another process (is uMailServer running?); stop the server before taking a backup so the copy is consistent", p)
		}
		if openErr != nil {
			continue // not a bbolt database: archived byte-for-byte
		}
		held = append(held, d)
	}
	return release, nil
}

// writeNewFile writes data to a file that must not already exist. Backup file
// names have one-second resolution, so a second backup in the same second
// must fail rather than truncate the earlier one (F4858).
func writeNewFile(path string, data []byte) error {
	f, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// encryptBackup encrypts tar.gz data using AES-256-GCM with scrypt key derivation
func (bm *BackupManager) encryptBackup(data []byte) ([]byte, error) {
	// Generate salt and nonce
	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Derive key from password using scrypt
	key, err := scrypt.Key([]byte(bm.password), salt, 1<<18, 8, 1, keySize) // N=2^18, r=8, p=1
	if err != nil {
		return nil, fmt.Errorf("failed to derive key: %w", err)
	}

	// Create AES-GCM cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Encrypt the data
	ciphertext := gcm.Seal(nil, nonce, data, nil)

	// Format: magic(12) + version(1) + salt(32) + nonce(12) + ciphertext
	result := make([]byte, 0, len(backupMagic)+1+saltSize+nonceSize+len(ciphertext))
	result = append(result, []byte(backupMagic)...)
	result = append(result, backupVersion)
	result = append(result, salt...)
	result = append(result, nonce...)
	result = append(result, ciphertext...)

	return result, nil
}

// decryptBackup decrypts AES-256-GCM encrypted backup data
func (bm *BackupManager) decryptBackup(data []byte) ([]byte, error) {
	if len(data) < len(backupMagic)+1+saltSize+nonceSize {
		return nil, fmt.Errorf("invalid backup file: too short")
	}

	// Parse header
	magic := string(data[:len(backupMagic)])
	if magic != backupMagic {
		// Not an encrypted backup, try as plain tar.gz
		return data, nil
	}

	version := int(data[len(backupMagic)])
	if version != backupVersion {
		return nil, fmt.Errorf("unsupported backup version: %d", version)
	}

	offset := len(backupMagic) + 1
	salt := data[offset : offset+saltSize]
	offset += saltSize
	nonce := data[offset : offset+nonceSize]
	offset += nonceSize
	ciphertext := data[offset:]

	// Derive key from password
	key, err := scrypt.Key([]byte(bm.password), salt, 1<<18, 8, 1, keySize)
	if err != nil {
		return nil, fmt.Errorf("failed to derive key: %w", err)
	}

	// Create AES-GCM cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Decrypt
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: wrong password?")
	}

	return plaintext, nil
}

// addFileWithHash copies a file to tar and records its hash
func (bm *BackupManager) addFileWithHash(tw *tar.Writer, path string, header *tar.Header) error {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return err
	}
	defer file.Close()

	// Calculate hash while copying
	h := sha256.New()
	writer := io.MultiWriter(tw, h)

	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	_, err = io.Copy(writer, file)
	if err != nil {
		return err
	}

	// Record hash
	bm.hashes = append(bm.hashes, fileHash{
		Path: header.Name,
		Hash: hex.EncodeToString(h.Sum(nil)),
		Size: header.Size,
	})

	return nil
}

// backupConfig adds configuration files to the backup
func (bm *BackupManager) backupConfig(tw *tar.Writer) error {
	configPath := bm.config.Server.DataDir + "/config"

	// Skip if config directory doesn't exist
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil
	}

	return filepath.Walk(configPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Create tar header
		relPath, err := filepath.Rel(bm.config.Server.DataDir, path)
		if err != nil {
			return err
		}

		header := &tar.Header{
			Name:    filepath.Join("config", relPath),
			Mode:    int64(info.Mode() & 0o7777),
			ModTime: info.ModTime(),
			Size:    info.Size(),
		}

		return bm.addFileWithHash(tw, path, header)
	})
}

// backupDatabase adds database files to the backup
func (bm *BackupManager) backupDatabase(tw *tar.Writer) error {
	dbPath := bm.config.Server.DataDir + "/umailserver.db"

	info, err := os.Stat(dbPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Database doesn't exist, skip
		}
		return err
	}

	header := &tar.Header{
		Name:    "database/umailserver.db",
		Mode:    int64(info.Mode() & 0o7777),
		ModTime: info.ModTime(),
		Size:    info.Size(),
	}

	return bm.addFileWithHash(tw, dbPath, header)
}

// backupMaildir adds maildir files to the backup. The live mail store is
// <DataDir>/mail (Maildir messages under mail/messages and the mailbox/UID
// database mail/mail.db, see server.New); <DataDir>/messages is the legacy
// location. Both are archived under "messages/" so that copying
// restore_temp/messages/* into the data directory restores them (F5450).
// The other stores the server keeps under DataDir travel the same way
// (F5550): queue/ (pending outbound message bodies; their queue entries are
// in umailserver.db), caldav/, carddav/, push/ (VAPID keys and
// subscriptions), vacation/ (auto-reply settings) and dkim/ (signing keys).
func (bm *BackupManager) backupMaildir(tw *tar.Writer) error {
	for _, sub := range []string{"messages", "mail", "queue", "caldav", "carddav", "push", "vacation", "dkim"} {
		if err := bm.backupDataSubdir(tw, sub); err != nil {
			return err
		}
	}
	return nil
}

// backupDataSubdir archives <DataDir>/<sub> under "messages/".
func (bm *BackupManager) backupDataSubdir(tw *tar.Writer, sub string) error {
	maildirPath := bm.config.Server.DataDir + "/" + sub

	// Skip if maildir directory doesn't exist
	if _, err := os.Stat(maildirPath); os.IsNotExist(err) {
		return nil
	}

	return filepath.Walk(maildirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories (but create them in tar)
		if info.IsDir() {
			relPath, err := filepath.Rel(bm.config.Server.DataDir, path)
			if err != nil {
				return err
			}

			header := &tar.Header{
				Name:     filepath.Join("messages", relPath) + "/",
				Mode:     int64(info.Mode() & 0o7777),
				ModTime:  info.ModTime(),
				Typeflag: tar.TypeDir,
			}

			return tw.WriteHeader(header)
		}

		// Create tar header for file
		relPath, err := filepath.Rel(bm.config.Server.DataDir, path)
		if err != nil {
			return err
		}

		header := &tar.Header{
			Name:    filepath.Join("messages", relPath),
			Mode:    int64(info.Mode() & 0o7777),
			ModTime: info.ModTime(),
			Size:    info.Size(),
		}

		return bm.addFileWithHash(tw, path, header)
	})
}

// createManifest creates a backup manifest
func (bm *BackupManager) createManifest(tw *tar.Writer, timestamp string) error {
	manifest := map[string]interface{}{
		"version":   "1.0.0",
		"timestamp": timestamp,
		"hostname":  bm.config.Server.Hostname,
		"data_dir":  bm.config.Server.DataDir,
		"contents": []string{
			"config/",
			"database/",
			"messages/",
		},
		"files": bm.hashes,
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}

	header := &tar.Header{
		Name:    "manifest.json",
		Mode:    0o600,
		ModTime: time.Now(),
		Size:    int64(len(data)),
	}

	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	_, err = tw.Write(data)
	return err
}

// Restore restores from a backup file
func (bm *BackupManager) Restore(backupFile string) error {
	fmt.Printf("Restoring from backup: %s\n", backupFile)

	fileData, err := os.ReadFile(filepath.Clean(backupFile))
	if err != nil {
		return fmt.Errorf("failed to open backup file: %w", err)
	}

	// Try to decrypt if password is set
	var tarData []byte
	if bm.password != "" {
		tarData, err = bm.decryptBackup(fileData)
		if err != nil {
			return fmt.Errorf("failed to decrypt backup: %w", err)
		}
		fmt.Println("Backup decrypted successfully.")
	} else {
		// Check if file is encrypted
		if len(fileData) > len(backupMagic) && string(fileData[:len(backupMagic)]) == backupMagic {
			return fmt.Errorf("backup is encrypted but no password provided; use SetPassword() first")
		}
		tarData = fileData
	}

	gr, err := gzip.NewReader(strings.NewReader(string(tarData)))
	if err != nil {
		return fmt.Errorf("failed to read gzip: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)

	// First, verify the backup by reading the manifest
	var manifest map[string]interface{}
	var expectedHashes []fileHash
	manifestFound := false

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar: %w", err)
		}

		if header.Name == "manifest.json" {
			data := make([]byte, header.Size)
			_, err := io.ReadFull(tr, data)
			if err != nil {
				return fmt.Errorf("failed to read manifest: %w", err)
			}
			if err := json.Unmarshal(data, &manifest); err != nil {
				return fmt.Errorf("failed to parse manifest: %w", err)
			}
			// Parse file hashes from manifest
			if files, ok := manifest["files"].([]interface{}); ok {
				for _, f := range files {
					if fileMap, ok := f.(map[string]interface{}); ok {
						h := fileHash{
							Path: getString(fileMap, "path"),
							Hash: getString(fileMap, "hash"),
							Size: getInt64(fileMap, "size"),
						}
						expectedHashes = append(expectedHashes, h)
					}
				}
			}
			manifestFound = true
			break
		}
	}

	if !manifestFound {
		return fmt.Errorf("invalid backup: manifest not found")
	}

	fmt.Printf("Backup from: %s\n", manifest["timestamp"])
	fmt.Printf("Hostname: %s\n", manifest["hostname"])

	// Verify file count
	if len(expectedHashes) > 0 {
		fmt.Printf("Backup contains %d files with integrity hashes\n", len(expectedHashes))
	}

	// Extract into a private staging directory and publish it as restore_temp
	// only after every member was written and verified, so a failed restore
	// leaves no partial tree behind (F4855). An existing non-empty
	// restore_temp holds an earlier restore; merging into it would mix stale
	// files with this backup, so refuse instead (F4856).
	restoreDir := filepath.Join(bm.config.Server.DataDir, "..", "restore_temp")
	if entries, err := os.ReadDir(restoreDir); err == nil {
		if len(entries) > 0 {
			return fmt.Errorf("restore directory %s already exists and is not empty; move or remove it first", restoreDir)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to inspect restore directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(restoreDir), 0o750); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	baseRestoreDir, err := os.MkdirTemp(filepath.Dir(restoreDir), ".restore_temp-")
	if err != nil {
		return fmt.Errorf("failed to create staging directory: %w", err)
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(baseRestoreDir)
		}
	}()
	seen := make(map[string]bool)

	// Re-create reader from tarData for extraction
	gr, err = gzip.NewReader(strings.NewReader(string(tarData)))
	if err != nil {
		return fmt.Errorf("failed to decompress backup: %w", err)
	}
	tr = tar.NewReader(gr)

	// Extract files
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar: %w", err)
		}

		// Validate filename to prevent path traversal attacks
		// Reject any path that could escape the restore_temp directory
		// Also normalize path separators for cross-platform compatibility
		sanitizedName := strings.ReplaceAll(header.Name, "/", string(filepath.Separator))
		if strings.Contains(sanitizedName, "..") || strings.HasPrefix(sanitizedName, string(filepath.Separator)) {
			return fmt.Errorf("invalid filename in tar: %s - path traversal detected", header.Name)
		}

		targetPath := filepath.Join(baseRestoreDir, sanitizedName)

		// Unconditional path traversal check: resolve and ensure target stays within base directory
		// #nosec G703 -- targetPath is validated here before any file operations
		absTargetPath, err := filepath.Abs(targetPath)
		if err != nil {
			return fmt.Errorf("failed to resolve target path: %w", err)
		}
		absBaseDir, err := filepath.Abs(baseRestoreDir)
		if err != nil {
			return fmt.Errorf("failed to resolve base directory: %w", err)
		}
		absTargetPath = filepath.Clean(absTargetPath)
		absBaseDir = filepath.Clean(absBaseDir)
		if !strings.HasPrefix(absTargetPath, absBaseDir+string(filepath.Separator)) && absTargetPath != absBaseDir {
			return fmt.Errorf("invalid filename: %s - would extract outside target directory", header.Name)
		}

		// #nosec G703 -- targetPath is validated above with filepath.Abs/Clean and prefix check
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o750); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if header.Mode < 0 || header.Mode > math.MaxUint32 {
				return fmt.Errorf("invalid mode in tar header: %d", header.Mode)
			}
			// #nosec G703 -- targetPath is validated above with filepath.Abs/Clean and prefix check
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode&0o7777)); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}

		case tar.TypeReg:
			// #nosec G703 G304 -- targetPath is validated above with filepath.Abs/Clean and prefix check
			outFile, err := os.Create(filepath.Clean(targetPath))
			if err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}

			// Calculate hash while extracting if we have expected hashes
			var writer io.Writer = outFile
			var h hash.Hash
			if len(expectedHashes) > 0 {
				h = sha256.New()
				writer = io.MultiWriter(outFile, h)
			}

			// #nosec G110 -- Copy is bounded by tar header.Size which is validated above
			if _, err := io.CopyN(writer, tr, header.Size); err != nil {
				_ = outFile.Close()
				return fmt.Errorf("failed to write file: %w", err)
			}
			_ = outFile.Close()

			// Set file permissions
			if header.Mode < 0 || header.Mode > math.MaxUint32 {
				return fmt.Errorf("invalid mode in tar header: %d", header.Mode)
			}
			// #nosec G703 -- targetPath validated before extraction with filepath.Abs/Clean and prefix check
			if err := os.Chmod(targetPath, os.FileMode(header.Mode&0o7777)); err != nil {
				return fmt.Errorf("failed to set permissions: %w", err)
			}

			// Verify hash if we have expected hashes
			if h != nil {
				computedHash := hex.EncodeToString(h.Sum(nil))
				for _, expected := range expectedHashes {
					if expected.Path == header.Name {
						seen[expected.Path] = true
						if computedHash != expected.Hash {
							// #nosec G703 -- targetPath validated before extraction with filepath.Abs/Clean and prefix check
							_ = os.Remove(targetPath)
							return fmt.Errorf("integrity check failed for %s: expected %s, got %s",
								header.Name, expected.Hash, computedHash)
						}
						fmt.Printf("  ✓ Verified: %s\n", header.Name)
						break
					}
				}
			}
		}
	}

	// The tar reader stops at the end-of-archive marker, so drain the gzip
	// stream to check its CRC-32/ISIZE footer. The manifest hashes do not
	// cover manifest.json itself or archives whose manifest lists no files
	// (F5451).
	if _, err := io.Copy(io.Discard, gr); err != nil {
		return fmt.Errorf("corrupt gzip stream: %w", err)
	}

	// Every file the manifest declares must have been restored (F4857).
	for _, expected := range expectedHashes {
		if !seen[expected.Path] {
			return fmt.Errorf("integrity check failed: %s is declared in the manifest but missing from the backup", expected.Path)
		}
	}

	if err := os.Chmod(baseRestoreDir, 0o750); err != nil {
		return fmt.Errorf("failed to set permissions: %w", err)
	}
	// An empty leftover restore_temp may be replaced; Remove fails on a
	// non-empty directory, so content created concurrently is never lost.
	if err := os.Remove(restoreDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("restore directory %s is not empty: %w", restoreDir, err)
	}
	if err := os.Rename(baseRestoreDir, restoreDir); err != nil {
		return fmt.Errorf("failed to publish restore directory: %w", err)
	}
	published = true

	fmt.Println("Backup extracted to restore_temp/")
	fmt.Println("To complete restore:")
	fmt.Println("1. Stop uMailServer")
	fmt.Println("2. Copy restore_temp/config/* to data directory")
	fmt.Println("3. Copy restore_temp/database/* to data directory")
	fmt.Println("4. Copy restore_temp/messages/* to data directory (mail, queue, calendars, contacts, push, vacation, DKIM keys)")
	fmt.Println("5. Start uMailServer")

	return nil
}

// Verify checks backup integrity without extracting files
func (bm *BackupManager) Verify(backupFile string) error {
	fmt.Printf("Verifying backup: %s\n", backupFile)

	fileData, err := os.ReadFile(filepath.Clean(backupFile))
	if err != nil {
		return fmt.Errorf("failed to open backup file: %w", err)
	}

	// Try to decrypt if password is set
	var tarData []byte
	if bm.password != "" {
		tarData, err = bm.decryptBackup(fileData)
		if err != nil {
			return fmt.Errorf("failed to decrypt backup: %w", err)
		}
		fmt.Println("Backup decrypted successfully.")
	} else {
		// Check if file is encrypted
		if len(fileData) > len(backupMagic) && string(fileData[:len(backupMagic)]) == backupMagic {
			return fmt.Errorf("backup is encrypted but no password provided; use SetPassword() first")
		}
		tarData = fileData
	}

	gr, err := gzip.NewReader(strings.NewReader(string(tarData)))
	if err != nil {
		return fmt.Errorf("failed to read gzip: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)

	// First pass: locate and parse the manifest. Backup() writes the
	// manifest as the LAST member, so it cannot be parsed in the same pass
	// that verifies the files — a single pass sees an empty expectedHashes
	// for every file entry and verifies nothing.
	var manifest map[string]interface{}
	var expectedHashes []fileHash
	manifestFound := false

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar: %w", err)
		}

		if header.Name == "manifest.json" {
			data := make([]byte, header.Size)
			_, err := io.ReadFull(tr, data)
			if err != nil {
				return fmt.Errorf("failed to read manifest: %w", err)
			}
			if err := json.Unmarshal(data, &manifest); err != nil {
				return fmt.Errorf("failed to parse manifest: %w", err)
			}
			// Parse file hashes from manifest
			if files, ok := manifest["files"].([]interface{}); ok {
				for _, f := range files {
					if fileMap, ok := f.(map[string]interface{}); ok {
						expectedHashes = append(expectedHashes, fileHash{
							Path: getString(fileMap, "path"),
							Hash: getString(fileMap, "hash"),
							Size: getInt64(fileMap, "size"),
						})
					}
				}
			}
			manifestFound = true
			fmt.Printf("Backup created: %s\n", manifest["timestamp"])
			fmt.Printf("Hostname: %s\n", manifest["hostname"])
			break
		}
	}

	if !manifestFound {
		return fmt.Errorf("invalid backup: manifest not found")
	}

	// Second pass: verify every regular member against the manifest hashes.
	gr, err = gzip.NewReader(strings.NewReader(string(tarData)))
	if err != nil {
		return fmt.Errorf("failed to decompress backup: %w", err)
	}
	defer gr.Close()
	tr = tar.NewReader(gr)

	filesVerified := 0
	filesFailed := 0
	seen := make(map[string]bool)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar: %w", err)
		}

		// Skip non-regular files
		if header.Typeflag != tar.TypeReg {
			continue
		}

		// Read file content to compute hash
		content := make([]byte, header.Size)
		_, err = io.ReadFull(tr, content)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", header.Name, err)
		}

		for _, expected := range expectedHashes {
			if expected.Path == header.Name {
				seen[expected.Path] = true
				computedHash := sha256.Sum256(content)
				if hex.EncodeToString(computedHash[:]) != expected.Hash {
					fmt.Printf("  ✗ FAILED: %s\n", header.Name)
					filesFailed++
				} else {
					fmt.Printf("  ✓ Verified: %s\n", header.Name)
					filesVerified++
				}
				break
			}
		}
	}

	// Check the gzip CRC-32/ISIZE footer, as Restore does (F5451).
	if _, err := io.Copy(io.Discard, gr); err != nil {
		return fmt.Errorf("corrupt gzip stream: %w", err)
	}

	// Every file the manifest declares must be present in the archive.
	// Without this coverage check a truncated or stripped archive — missing
	// the database, for example — verifies as intact.
	for _, expected := range expectedHashes {
		if !seen[expected.Path] {
			fmt.Printf("  ✗ MISSING: %s\n", expected.Path)
			filesFailed++
		}
	}

	fmt.Printf("\nVerification complete: %d files verified, %d failed\n", filesVerified, filesFailed)
	if filesFailed > 0 {
		return fmt.Errorf("backup verification failed: %d files have incorrect hashes", filesFailed)
	}
	return nil
}

// CleanupOldBackups removes backups older than the specified retention days
func (bm *BackupManager) CleanupOldBackups(backupPath string, retentionDays int) (int, error) {
	if retentionDays <= 0 {
		return 0, fmt.Errorf("retention days must be positive")
	}

	entries, err := os.ReadDir(backupPath)
	if err != nil {
		return 0, fmt.Errorf("failed to read backup directory: %w", err)
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	deleted := 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if filepath.Ext(name) != ".gz" && !strings.HasSuffix(name, ".tar.gz.enc") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			backupFile := filepath.Join(backupPath, name)
			if err := os.Remove(backupFile); err != nil {
				fmt.Printf("Failed to delete %s: %v\n", name, err)
				continue
			}
			fmt.Printf("Deleted old backup: %s (age: %s)\n", name, time.Since(info.ModTime()).Round(time.Hour))
			deleted++
		}
	}

	return deleted, nil
}

// ListBackups lists available backups in a directory
func (bm *BackupManager) ListBackups(backupPath string) ([]BackupInfo, error) {
	entries, err := os.ReadDir(backupPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read backup directory: %w", err)
	}

	var backups []BackupInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if filepath.Ext(name) != ".gz" && !strings.HasSuffix(name, ".tar.gz.enc") {
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
			Path:     filepath.Join(backupPath, name),
		})
	}

	return backups, nil
}

// BackupInfo holds information about a backup
type BackupInfo struct {
	Filename string
	Size     int64
	ModTime  time.Time
	Path     string
}

// getString extracts a string value from a map
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// getInt64 extracts an int64 value from a map
func getInt64(m map[string]interface{}, key string) int64 {
	switch v := m[key].(type) {
	case int64:
		return v
	case float64:
		return int64(v)
	case int:
		return int64(v)
	}
	return 0
}
