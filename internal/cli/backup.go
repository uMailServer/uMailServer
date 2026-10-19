package cli

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
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

// maxManifestSize bounds the manifest allocation: the size comes from the
// archive header, so a crafted archive could otherwise force a huge
// allocation (F5913).
const maxManifestSize = 256 << 20

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

	// Stream tar -> gzip -> (AES-GCM chunks) -> file so memory use does not
	// grow with the archive (F6460). The file must not already exist; a failed
	// backup leaves no partial file behind.
	fmt.Printf("Creating backup archive...\n")
	out, err := os.OpenFile(filepath.Clean(backupFile), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("failed to write backup: %w", err)
	}
	ok := false
	defer func() {
		if !ok {
			_ = out.Close()
			_ = os.Remove(filepath.Clean(backupFile))
		}
	}()
	bw := bufio.NewWriterSize(out, 256<<10)
	var sink io.Writer = bw
	var enc *streamEncryptWriter
	if bm.password != "" {
		fmt.Printf("Encrypting backup with AES-256-GCM (chunked)...\n")
		enc, err = newStreamEncryptWriter(bw, bm.password, defaultChunkSize)
		if err != nil {
			return fmt.Errorf("failed to encrypt backup: %w", err)
		}
		sink = enc
	} else {
		// Warn about unencrypted backup
		fmt.Printf("WARNING: Backup is NOT ENCRYPTED. Sensitive data may be exposed.\n")
		fmt.Printf("         Use SetPassword() to enable AES-256-GCM encryption.\n")
	}
	gw := gzip.NewWriter(sink)
	tw := tar.NewWriter(gw)

	fmt.Println("  Adding configuration...")
	if err := bm.backupConfig(tw); err != nil {
		return fmt.Errorf("failed to backup config: %w", err)
	}
	fmt.Println("  Adding database...")
	if err := bm.backupDatabase(tw); err != nil {
		return fmt.Errorf("failed to backup database: %w", err)
	}
	fmt.Println("  Adding maildir...")
	if err := bm.backupMaildir(tw); err != nil {
		return fmt.Errorf("failed to backup maildir: %w", err)
	}
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
	if enc != nil {
		if err := enc.Close(); err != nil {
			return fmt.Errorf("failed to encrypt backup: %w", err)
		}
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("failed to write backup: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("failed to write backup: %w", err)
	}
	ok = true

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
		_ = os.Remove(filepath.Clean(path)) // F6401: no truncated backup left behind
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(filepath.Clean(path))
		return err
	}
	return nil
}

// encryptBackup encrypts data into the chunked v2 envelope (see
// backup_stream.go). Backup itself streams; this in-memory form is kept for
// callers and tests that hold the data already.
func (bm *BackupManager) encryptBackup(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := newStreamEncryptWriter(&buf, bm.password, defaultChunkSize)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// decryptBackup decrypts an in-memory envelope: the legacy single-buffer v1
// format or the chunked v2 format. Data without the magic is returned as is.
func (bm *BackupManager) decryptBackup(data []byte) ([]byte, error) {
	if len(data) < len(backupMagic)+1+saltSize+nonceSize {
		return nil, fmt.Errorf("invalid backup file: too short")
	}
	if string(data[:len(backupMagic)]) != backupMagic {
		// Not an encrypted backup, try as plain tar.gz
		return data, nil
	}
	switch version := int(data[len(backupMagic)]); version {
	case backupVersion:
		return bm.decryptBackupV1(data)
	case backupVersionStream:
		return decryptStreamBytes(data, bm.password)
	default:
		return nil, fmt.Errorf("unsupported backup version: %d", version)
	}
}

// decryptBackupV1 reads the legacy format:
// magic | version(1) | salt(32) | nonce(12) | single AES-GCM ciphertext.
func (bm *BackupManager) decryptBackupV1(data []byte) ([]byte, error) {
	offset := len(backupMagic) + 1
	salt := data[offset : offset+saltSize]
	offset += saltSize
	nonce := data[offset : offset+nonceSize]
	offset += nonceSize
	ciphertext := data[offset:]

	key, err := deriveBackupKey(bm.password, salt)
	if err != nil {
		return nil, err
	}
	gcm, err := newBackupGCM(key)
	if err != nil {
		return nil, err
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: wrong password?")
	}
	return plaintext, nil
}

// backupSource is an opened backup yielding the plain tar.gz stream.
type backupSource struct {
	f *os.File
	r io.Reader
}

func (b *backupSource) Close() error { return b.f.Close() }

// openBackupSource opens a backup file and returns its plaintext tar.gz
// stream. Plain and v2 files are streamed; only legacy v1 files must be
// buffered (their single GCM tag covers the whole archive).
func (bm *BackupManager) openBackupSource(path string) (*backupSource, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("failed to open backup file: %w", err)
	}
	br := bufio.NewReaderSize(f, 256<<10)
	head, _ := br.Peek(len(backupMagic) + 1)
	encrypted := len(head) > len(backupMagic) && string(head[:len(backupMagic)]) == backupMagic
	if !encrypted {
		return &backupSource{f: f, r: br}, nil
	}
	if bm.password == "" {
		_ = f.Close()
		return nil, fmt.Errorf("backup is encrypted but no password provided; use SetPassword() first")
	}
	switch v := int(head[len(backupMagic)]); v {
	case backupVersionStream:
		sr, err := newStreamDecryptReader(br, bm.password)
		if err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("failed to decrypt backup: %w", err)
		}
		return &backupSource{f: f, r: sr}, nil
	case backupVersion:
		all, err := io.ReadAll(br)
		if err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("failed to open backup file: %w", err)
		}
		plain, err := bm.decryptBackup(all)
		if err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("failed to decrypt backup: %w", err)
		}
		return &backupSource{f: f, r: bytes.NewReader(plain)}, nil
	default:
		_ = f.Close()
		return nil, fmt.Errorf("failed to decrypt backup: unsupported backup version: %d", v)
	}
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

// skipNonRegular reports whether a directory entry of this mode is not
// archived: symlinks (F5912), sockets, devices and irregular files (F7490).
func skipNonRegular(m os.FileMode) bool {
	return m&(os.ModeSymlink|os.ModeSocket|os.ModeDevice|os.ModeCharDevice|os.ModeIrregular) != 0
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

		// Skip directories and symlinks: a symlink is opened through the
		// link but its header size is the link-target length
		// (tar.ErrWriteTooLong) (F5912).
		// Sockets and devices are skipped too: opening one fails (F7490).
		// Named pipes are left alone: TestBackupHoldsDatabaseLock parks
		// Backup on one to gate its ordering.
		if info.IsDir() || skipNonRegular(info.Mode()) {
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

		// Symlinks are not archived (F5912); neither are sockets and
		// devices, which cannot be read as file content (F7490).
		if skipNonRegular(info.Mode()) {
			return nil
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

	src, err := bm.openBackupSource(backupFile)
	if err != nil {
		return err
	}
	defer src.Close()
	if bm.password != "" {
		fmt.Println("Backup decrypted successfully.")
	}

	gr, err := gzip.NewReader(src.r)
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
			if header.Size < 0 || header.Size > maxManifestSize {
				return fmt.Errorf("invalid backup: manifest size %d out of range", header.Size)
			}
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
	src2, err := bm.openBackupSource(backupFile)
	if err != nil {
		return err
	}
	defer src2.Close()
	gr, err = gzip.NewReader(src2.r)
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
		if hasDotDotElement(sanitizedName) || strings.HasPrefix(sanitizedName, string(filepath.Separator)) {
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
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode&0o777)|0o700); err != nil {
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
			if err := os.Chmod(targetPath, os.FileMode(header.Mode&0o777)); err != nil {
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
	// Drain the underlying stream so the final authenticated chunk of an
	// encrypted backup is checked (truncation/reorder/tamper, F6460).
	if _, err := io.Copy(io.Discard, src2.r); err != nil {
		return fmt.Errorf("corrupt backup stream: %w", err)
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

	printRestoreSteps(restoreDir, bm.config.Server.DataDir)

	return nil
}

// printRestoreSteps tells the operator where the archive was extracted (the
// real path: restore_temp sits next to data_dir, not in the working directory)
// and which of its parts to copy into the data directory. Only the parts the
// archive holds are listed; a backup has no config/ directory unless one
// existed in the data directory (F7760).
func printRestoreSteps(restoreDir, dataDir string) {
	shown := restoreDir
	if abs, err := filepath.Abs(restoreDir); err == nil {
		shown = abs
	}
	fmt.Printf("Backup extracted to %s\n", shown)
	fmt.Println("To complete restore:")
	fmt.Println("1. Stop uMailServer")
	step := 2
	for _, part := range []struct{ dir, note string }{
		{"config", ""},
		{"database", ""},
		{"messages", " (mail, queue, calendars, contacts, push, vacation, DKIM keys)"},
	} {
		if info, err := os.Stat(filepath.Join(restoreDir, part.dir)); err == nil && info.IsDir() {
			fmt.Printf("%d. Copy %s/* to the data directory %s%s\n", step, filepath.Join(shown, part.dir), dataDir, part.note)
			step++
		}
	}
	fmt.Printf("%d. Start uMailServer\n", step)
}

// Verify checks backup integrity without extracting files
func (bm *BackupManager) Verify(backupFile string) error {
	fmt.Printf("Verifying backup: %s\n", backupFile)

	src, err := bm.openBackupSource(backupFile)
	if err != nil {
		return err
	}
	defer src.Close()
	if bm.password != "" {
		fmt.Println("Backup decrypted successfully.")
	}

	gr, err := gzip.NewReader(src.r)
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
			if header.Size < 0 || header.Size > maxManifestSize {
				return fmt.Errorf("invalid backup: manifest size %d out of range", header.Size)
			}
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
	src2, err := bm.openBackupSource(backupFile)
	if err != nil {
		return err
	}
	defer src2.Close()
	gr, err = gzip.NewReader(src2.r)
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

		// F6400: stream the member through the hash; never allocate the
		// attacker-declared header.Size up front.
		hasher := sha256.New()
		if _, err = io.CopyN(hasher, tr, header.Size); err != nil {
			return fmt.Errorf("failed to read file %s: %w", header.Name, err)
		}
		computed := hex.EncodeToString(hasher.Sum(nil))

		for _, expected := range expectedHashes {
			if expected.Path == header.Name {
				seen[expected.Path] = true
				if computed != expected.Hash {
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
	// Drain the underlying stream so the final authenticated chunk of an
	// encrypted backup is checked (truncation/reorder/tamper, F6460).
	if _, err := io.Copy(io.Discard, src2.r); err != nil {
		return fmt.Errorf("corrupt backup stream: %w", err)
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

// hasDotDotElement reports whether any path element is exactly "..". A name
// that merely contains two dots (a..b.eml) is not a traversal (F7322).
func hasDotDotElement(name string) bool {
	for _, el := range strings.Split(name, string(filepath.Separator)) {
		if el == ".." {
			return true
		}
	}
	return false
}
