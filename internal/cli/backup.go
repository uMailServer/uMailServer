package cli

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/umailserver/umailserver/internal/config"
)

// BackupManager handles backup and restore operations
type BackupManager struct {
	config *config.Config
}

// NewBackupManager creates a new backup manager
func NewBackupManager(cfg *config.Config) *BackupManager {
	return &BackupManager{
		config: cfg,
	}
}

// Backup creates a full backup of the server
func (bm *BackupManager) Backup(backupPath string) error {
	timestamp := time.Now().Format("20060102_150405")
	backupFile := filepath.Join(backupPath, fmt.Sprintf("umailserver_backup_%s.tar.gz", timestamp))

	// Create backup directory
	if err := os.MkdirAll(backupPath, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Create tar.gz file
	file, err := os.Create(backupFile)
	if err != nil {
		return fmt.Errorf("failed to create backup file: %w", err)
	}
	defer file.Close()

	gw := gzip.NewWriter(file)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	fmt.Printf("Creating backup: %s\n", backupFile)

	// Backup config
	fmt.Println("Backing up configuration...")
	if err := bm.backupConfig(tw); err != nil {
		return fmt.Errorf("failed to backup config: %w", err)
	}

	// Backup database
	fmt.Println("Backing up database...")
	if err := bm.backupDatabase(tw); err != nil {
		return fmt.Errorf("failed to backup database: %w", err)
	}

	// Backup maildir
	fmt.Println("Backing up maildir...")
	if err := bm.backupMaildir(tw); err != nil {
		return fmt.Errorf("failed to backup maildir: %w", err)
	}

	// Create backup manifest
	fmt.Println("Creating backup manifest...")
	if err := bm.createManifest(tw, timestamp); err != nil {
		return fmt.Errorf("failed to create manifest: %w", err)
	}

	fmt.Printf("Backup completed successfully: %s\n", backupFile)
	return nil
}

// backupConfig adds configuration files to the backup
func (bm *BackupManager) backupConfig(tw *tar.Writer) error {
	configPath := bm.config.Server.DataDir + "/config"

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
			Mode:    int64(info.Mode()),
			ModTime: info.ModTime(),
			Size:    info.Size(),
		}

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		// Copy file content
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		_, err = io.Copy(tw, file)
		return err
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

	file, err := os.Open(dbPath)
	if err != nil {
		return err
	}
	defer file.Close()

	header := &tar.Header{
		Name:    "database/umailserver.db",
		Mode:    int64(info.Mode()),
		ModTime: info.ModTime(),
		Size:    info.Size(),
	}

	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	_, err = io.Copy(tw, file)
	return err
}

// backupMaildir adds maildir files to the backup
func (bm *BackupManager) backupMaildir(tw *tar.Writer) error {
	maildirPath := bm.config.Server.DataDir + "/messages"

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
				Mode:     int64(info.Mode()),
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
			Mode:    int64(info.Mode()),
			ModTime: info.ModTime(),
			Size:    info.Size(),
		}

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		// Copy file content
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		_, err = io.Copy(tw, file)
		return err
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
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}

	header := &tar.Header{
		Name:    "manifest.json",
		Mode:    0644,
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

	file, err := os.Open(backupFile)
	if err != nil {
		return fmt.Errorf("failed to open backup file: %w", err)
	}
	defer file.Close()

	gr, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("failed to read gzip: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)

	// First, verify the backup by reading the manifest
	var manifest map[string]interface{}
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
			manifestFound = true
			break
		}
	}

	if !manifestFound {
		return fmt.Errorf("invalid backup: manifest not found")
	}

	fmt.Printf("Backup from: %s\n", manifest["timestamp"])
	fmt.Printf("Hostname: %s\n", manifest["hostname"])

	// Reset file to beginning
	file.Seek(0, 0)
	gr, _ = gzip.NewReader(file)
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

		targetPath := filepath.Join(bm.config.Server.DataDir, "..", "restore_temp", header.Name)

		// Create parent directory
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}

		case tar.TypeReg:
			outFile, err := os.Create(targetPath)
			if err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}

			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return fmt.Errorf("failed to write file: %w", err)
			}
			outFile.Close()

			// Set file permissions
			os.Chmod(targetPath, os.FileMode(header.Mode))
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
