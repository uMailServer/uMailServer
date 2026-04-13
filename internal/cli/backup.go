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
	"fmt"
	"hash"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

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
		if err := os.WriteFile(backupFile, encrypted, 0o600); err != nil {
			return fmt.Errorf("failed to write encrypted backup: %w", err)
		}
	} else {
		// Warn about unencrypted backup
		fmt.Printf("WARNING: Backup is NOT ENCRYPTED. Sensitive data may be exposed.\n")
		fmt.Printf("         Use SetPassword() to enable AES-256-GCM encryption.\n")

		if err := os.WriteFile(backupFile, tarData, 0o600); err != nil {
			return fmt.Errorf("failed to write backup: %w", err)
		}
	}

	fmt.Printf("Backup completed successfully: %s\n", backupFile)
	return nil
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

// backupMaildir adds maildir files to the backup
func (bm *BackupManager) backupMaildir(tw *tar.Writer) error {
	maildirPath := bm.config.Server.DataDir + "/messages"

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

		baseRestoreDir := filepath.Join(bm.config.Server.DataDir, "..", "restore_temp")
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

	fmt.Println("Backup extracted to restore_temp/")
	fmt.Println("To complete restore:")
	fmt.Println("1. Stop uMailServer")
	fmt.Println("2. Copy restore_temp/config/* to data directory")
	fmt.Println("3. Copy restore_temp/database/* to data directory")
	fmt.Println("4. Copy restore_temp/messages/* to data directory")
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

	// Read manifest and verify all file hashes
	var manifest map[string]interface{}
	var expectedHashes []fileHash
	manifestFound := false
	filesVerified := 0
	filesFailed := 0

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
			fmt.Printf("Backup created: %s\n", manifest["timestamp"])
			fmt.Printf("Hostname: %s\n", manifest["hostname"])
			continue
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

		// Verify hash if we have expected hashes
		if len(expectedHashes) > 0 {
			computedHash := sha256.Sum256(content)
			computedHashHex := hex.EncodeToString(computedHash[:])
			for _, expected := range expectedHashes {
				if expected.Path == header.Name {
					if computedHashHex != expected.Hash {
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
	}

	if !manifestFound {
		return fmt.Errorf("invalid backup: manifest not found")
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
		if filepath.Ext(name) != ".gz" && !strings.HasSuffix(name, ".tar.gz") {
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
		if filepath.Ext(name) != ".gz" && !strings.HasSuffix(name, ".tar.gz") {
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
