package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/umailserver/umailserver/internal/backup"
	"github.com/umailserver/umailserver/internal/storage"
)

// handleBackupPath handles /api/v1/backups/{id} subpaths
func (s *Server) handleBackupPath(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Handle backup job run trigger
	if strings.HasSuffix(path, "/run") && r.Method == http.MethodPost {
		jobID := strings.TrimSuffix(strings.TrimPrefix(path, "/api/v1/backup-jobs/"), "/run")
		s.handleBackupJobRunHTTP(w, r, jobID)
		return
	}

	// Route based on path and method
	switch {
	case strings.Contains(path, "/backups/per-"):
		// Already handled by specific handlers
		http.Error(w, "Not found", http.StatusNotFound)
	case strings.HasPrefix(path, "/api/v1/backups/"):
		backupID := strings.TrimPrefix(path, "/api/v1/backups/")
		if backupID == "" {
			http.Error(w, "Backup ID required", http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodGet:
			s.handleBackupGetHTTP(w, r, backupID)
		case http.MethodDelete:
			s.handleBackupDeleteHTTP(w, r, backupID)
		case http.MethodPost:
			// Check if it's verify or restore
			if strings.HasSuffix(backupID, "/verify") {
				actualID := strings.TrimSuffix(backupID, "/verify")
				s.handleBackupVerifyHTTP(w, r, actualID)
			} else if strings.HasSuffix(backupID, "/restore") {
				actualID := strings.TrimSuffix(backupID, "/restore")
				s.handleBackupRestoreHTTP(w, r, actualID)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	default:
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

// handleBackupJobPath handles /api/v1/backup-jobs/{id} subpaths
func (s *Server) handleBackupJobPath(w http.ResponseWriter, r *http.Request) {
	jobID := strings.TrimPrefix(r.URL.Path, "/api/v1/backup-jobs/")
	if jobID == "" {
		http.Error(w, "Job ID required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleBackupJobGetHTTP(w, r, jobID)
	case http.MethodPut:
		s.handleBackupJobUpdateHTTP(w, r, jobID)
	case http.MethodDelete:
		s.handleBackupJobDeleteHTTP(w, r, jobID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// HTTP handler variants that take path parameters
func (s *Server) handleBackupGetHTTP(w http.ResponseWriter, r *http.Request, backupID string) {
	manifest, err := s.mailDB.GetBackupManifest(backupID)
	if err != nil {
		http.Error(w, "Backup not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(manifest)
}

func (s *Server) handleBackupDeleteHTTP(w http.ResponseWriter, r *http.Request, backupID string) {
	err := s.mailDB.DeleteBackupManifest(backupID)
	if err != nil {
		http.Error(w, "Failed to delete backup", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Backup deleted"})
}

func (s *Server) handleBackupVerifyHTTP(w http.ResponseWriter, r *http.Request, backupID string) {
	manifest, err := s.mailDB.GetBackupManifest(backupID)
	if err != nil {
		http.Error(w, "Backup not found", http.StatusNotFound)
		return
	}

	if s.backupMgr == nil {
		http.Error(w, "Backup manager not configured", http.StatusServiceUnavailable)
		return
	}

	path := manifest.Path
	if path == "" {
		path = filepath.Join(s.config.DataDir, "backups", "full", manifest.Filename)
	}

	verified, err := s.backupMgr.Verify(path)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":       backupID,
			"verified": false,
			"message":  fmt.Sprintf("Verification failed: %v", err),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":       backupID,
		"verified": true,
		"checksum": verified.Checksum,
		"message":  "Backup verified successfully",
	})
}

func (s *Server) handleBackupRestoreHTTP(w http.ResponseWriter, r *http.Request, backupID string) {
	var req struct {
		Mode       string `json:"mode"`
		TargetUser string `json:"target_user"`
		Overwrite  bool   `json:"overwrite"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if s.backupMgr == nil {
		http.Error(w, "Backup manager not configured", http.StatusServiceUnavailable)
		return
	}

	manifest, err := s.mailDB.GetBackupManifest(backupID)
	if err != nil {
		http.Error(w, "Backup not found", http.StatusNotFound)
		return
	}

	path := manifest.Path
	if path == "" {
		path = filepath.Join(s.config.DataDir, "backups", "full", manifest.Filename)
	}

	opts := backup.RestoreOptions{Overwrite: req.Overwrite}
	switch req.Mode {
	case "overwrite":
		opts.Mode = backup.RestoreModeOverwrite
	case "merge":
		opts.Mode = backup.RestoreModeMerge
	case "different-user":
		opts.Mode = backup.RestoreModeDifferent
		opts.TargetUser = req.TargetUser
	default:
		opts.Mode = backup.RestoreModeOverwrite
	}

	if err := s.backupMgr.Restore(path, opts); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      backupID,
			"status":  "failed",
			"message": fmt.Sprintf("Restore failed: %v", err),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":      backupID,
		"status":  "completed",
		"message": "Restore completed successfully",
	})
}

func (s *Server) handleBackupJobGetHTTP(w http.ResponseWriter, r *http.Request, jobID string) {
	job, err := s.mailDB.GetBackupJob(jobID)
	if err != nil {
		http.Error(w, "Backup job not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(job)
}

func (s *Server) handleBackupJobUpdateHTTP(w http.ResponseWriter, r *http.Request, jobID string) {
	var job storage.BackupJob
	if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	job.ID = jobID
	err := s.mailDB.UpdateBackupJob(&job)
	if err != nil {
		http.Error(w, "Failed to update backup job", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"job": job, "message": "Backup job updated"})
}

func (s *Server) handleBackupJobDeleteHTTP(w http.ResponseWriter, r *http.Request, jobID string) {
	err := s.mailDB.DeleteBackupJob(jobID)
	if err != nil {
		http.Error(w, "Failed to delete backup job", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Backup job deleted"})
}

func (s *Server) handleBackupJobRunHTTP(w http.ResponseWriter, r *http.Request, jobID string) {
	job, err := s.mailDB.GetBackupJob(jobID)
	if err != nil {
		http.Error(w, "Backup job not found", http.StatusNotFound)
		return
	}

	if s.backupMgr == nil {
		http.Error(w, "Backup manager not configured", http.StatusServiceUnavailable)
		return
	}

	backupDir := filepath.Join(s.config.DataDir, "backups", "full")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		http.Error(w, "Failed to create backup directory", http.StatusInternalServerError)
		return
	}

	opts := backup.BackupOptions{Encrypted: false}
	var destPath string

	switch job.Type {
	case "per-user":
		backupID := fmt.Sprintf("backup_peruser_%s_%d.tar.gz", job.Target, time.Now().UnixNano())
		destPath = filepath.Join(backupDir, backupID)
		err = s.backupMgr.BackupUser(job.Target, destPath, opts)
	case "per-mailbox":
		parts := strings.SplitN(job.Target, "/", 2)
		if len(parts) != 2 {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":      jobID,
				"status":  "failed",
				"message": "per-mailbox target must be user/mailbox format",
			})
			return
		}
		backupID := fmt.Sprintf("backup_permailbox_%s_%d.tar.gz", job.Target, time.Now().UnixNano())
		destPath = filepath.Join(backupDir, backupID)
		err = s.backupMgr.BackupMailbox(parts[0], parts[1], destPath, opts)
	default:
		backupID := fmt.Sprintf("backup_full_%d.tar.gz", time.Now().UnixNano())
		destPath = filepath.Join(backupDir, backupID)
		err = s.backupMgr.BackupFull(destPath, opts)
	}

	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      jobID,
			"status":  "failed",
			"message": fmt.Sprintf("Backup job failed: %v", err),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":      jobID,
		"status":  "completed",
		"path":    destPath,
		"message": "Backup job completed successfully",
	})
}

func (s *Server) handleBackupList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Optional target filter
	target := r.URL.Query().Get("target")

	manifests, err := s.mailDB.ListBackupManifests(target)
	if err != nil {
		http.Error(w, "Failed to list backups", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"backups": manifests,
	})
}

// handleBackupCreate handles POST /api/v1/backups - trigger immediate backup
func (s *Server) handleBackupCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Type    string `json:"type"`   // full, per-user, per-mailbox
		Target  string `json:"target"` // user or mailbox name
		Encrypt bool   `json:"encrypt"`
		Path    string `json:"path"` // custom destination path
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if s.backupMgr == nil {
		http.Error(w, "Backup manager not configured", http.StatusServiceUnavailable)
		return
	}

	backupDir := filepath.Join(s.config.DataDir, "backups", "full")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		http.Error(w, "Failed to create backup directory", http.StatusInternalServerError)
		return
	}

	opts := backup.BackupOptions{Encrypted: req.Encrypt}
	var destPath string
	var err error

	switch req.Type {
	case "per-user":
		backupID := fmt.Sprintf("backup_peruser_%s_%d.tar.gz", req.Target, time.Now().UnixNano())
		destPath = filepath.Join(backupDir, backupID)
		err = s.backupMgr.BackupUser(req.Target, destPath, opts)
	case "per-mailbox":
		http.Error(w, "per-mailbox requires user/mailbox path in target", http.StatusBadRequest)
		return
	default:
		backupID := fmt.Sprintf("backup_full_%d.tar.gz", time.Now().UnixNano())
		destPath = filepath.Join(backupDir, backupID)
		err = s.backupMgr.BackupFull(destPath, opts)
	}

	if err != nil {
		http.Error(w, fmt.Sprintf("Backup failed: %v", err), http.StatusInternalServerError)
		return
	}

	manifest, _ := s.backupMgr.GetBackupInfo(destPath)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":       manifest.ID,
		"filename": manifest.Filename,
		"size":     manifest.Size,
		"path":     destPath,
		"status":   "completed",
		"message":  "Backup completed successfully",
	})
}

// handleBackupGet handles GET /api/v1/backups/{id} - get backup info
func (s *Server) handleBackupGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	backupID := extractPathSuffix(r.URL.Path, "/api/v1/backups/")
	if backupID == "" {
		http.Error(w, "Backup ID required", http.StatusBadRequest)
		return
	}

	manifest, err := s.mailDB.GetBackupManifest(backupID)
	if err != nil {
		http.Error(w, "Backup not found", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(manifest)
}

// handleBackupDelete handles DELETE /api/v1/backups/{id} - delete a backup
func (s *Server) handleBackupDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	backupID := extractPathSuffix(r.URL.Path, "/api/v1/backups/")
	if backupID == "" {
		http.Error(w, "Backup ID required", http.StatusBadRequest)
		return
	}

	err := s.mailDB.DeleteBackupManifest(backupID)
	if err != nil {
		http.Error(w, "Failed to delete backup", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Backup deleted",
	})
}

// handleBackupVerify handles POST /api/v1/backups/{id}/verify - verify backup integrity
func (s *Server) handleBackupVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	backupID := extractPathSuffix(r.URL.Path, "/api/v1/backups/")
	if backupID == "" {
		http.Error(w, "Backup ID required", http.StatusBadRequest)
		return
	}

	manifest, err := s.mailDB.GetBackupManifest(backupID)
	if err != nil {
		http.Error(w, "Backup not found", http.StatusNotFound)
		return
	}

	if s.backupMgr == nil {
		http.Error(w, "Backup manager not configured", http.StatusServiceUnavailable)
		return
	}

	path := manifest.Path
	if path == "" {
		path = filepath.Join(s.config.DataDir, "backups", "full", manifest.Filename)
	}

	verified, err := s.backupMgr.Verify(path)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":       backupID,
			"verified": false,
			"message":  fmt.Sprintf("Verification failed: %v", err),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":       backupID,
		"verified": true,
		"checksum": verified.Checksum,
		"message":  "Backup verified successfully",
	})
}

// handleBackupRestore handles POST /api/v1/backups/{id}/restore - restore from backup
func (s *Server) handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	backupID := extractPathSuffix(r.URL.Path, "/api/v1/backups/")
	if backupID == "" {
		http.Error(w, "Backup ID required", http.StatusBadRequest)
		return
	}

	var req struct {
		Mode       string `json:"mode"`        // overwrite, merge, different-user
		TargetUser string `json:"target_user"` // for different-user mode
		Overwrite  bool   `json:"overwrite"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if s.backupMgr == nil {
		http.Error(w, "Backup manager not configured", http.StatusServiceUnavailable)
		return
	}

	manifest, err := s.mailDB.GetBackupManifest(backupID)
	if err != nil {
		http.Error(w, "Backup not found", http.StatusNotFound)
		return
	}

	path := manifest.Path
	if path == "" {
		path = filepath.Join(s.config.DataDir, "backups", "full", manifest.Filename)
	}

	opts := backup.RestoreOptions{Overwrite: req.Overwrite}
	switch req.Mode {
	case "overwrite":
		opts.Mode = backup.RestoreModeOverwrite
	case "merge":
		opts.Mode = backup.RestoreModeMerge
	case "different-user":
		opts.Mode = backup.RestoreModeDifferent
		opts.TargetUser = req.TargetUser
	default:
		opts.Mode = backup.RestoreModeOverwrite
	}

	if err := s.backupMgr.Restore(path, opts); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      backupID,
			"status":  "failed",
			"message": fmt.Sprintf("Restore failed: %v", err),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":      backupID,
		"status":  "completed",
		"message": "Restore completed successfully",
	})
}

// handlePerUserBackup handles POST /api/v1/backups/per-user/{user} - per-user backup
func (s *Server) handlePerUserBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user := extractPathSuffix(r.URL.Path, "/api/v1/backups/per-user/")
	if user == "" {
		http.Error(w, "User required", http.StatusBadRequest)
		return
	}

	if s.backupMgr == nil {
		http.Error(w, "Backup manager not configured", http.StatusServiceUnavailable)
		return
	}

	backupDir := filepath.Join(s.config.DataDir, "backups", "per-user", user)
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		http.Error(w, "Failed to create backup directory", http.StatusInternalServerError)
		return
	}

	backupID := fmt.Sprintf("backup_peruser_%s_%d.tar.gz", user, time.Now().UnixNano())
	destPath := filepath.Join(backupDir, backupID)
	opts := backup.BackupOptions{Encrypted: false}

	if err := s.backupMgr.BackupUser(user, destPath, opts); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      user,
			"target":  user,
			"status":  "failed",
			"message": fmt.Sprintf("Per-user backup failed: %v", err),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":      backupID,
		"target":  user,
		"path":    destPath,
		"status":  "completed",
		"message": "Per-user backup completed successfully",
	})
}

// handlePerMailboxBackup handles POST /api/v1/backups/per-mailbox/{user}/{mailbox}
func (s *Server) handlePerMailboxBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Path: /api/v1/backups/per-mailbox/{user}/{mailbox}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/backups/per-mailbox/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		http.Error(w, "User and mailbox required", http.StatusBadRequest)
		return
	}

	user, mailbox := parts[0], parts[1]

	if s.backupMgr == nil {
		http.Error(w, "Backup manager not configured", http.StatusServiceUnavailable)
		return
	}

	backupDir := filepath.Join(s.config.DataDir, "backups", "per-mailbox", user)
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		http.Error(w, "Failed to create backup directory", http.StatusInternalServerError)
		return
	}

	backupID := fmt.Sprintf("backup_permailbox_%s_%s_%d.tar.gz", user, mailbox, time.Now().UnixNano())
	destPath := filepath.Join(backupDir, backupID)
	opts := backup.BackupOptions{Encrypted: false}

	if err := s.backupMgr.BackupMailbox(user, mailbox, destPath, opts); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      backupID,
			"target":  fmt.Sprintf("%s/%s", user, mailbox),
			"status":  "failed",
			"message": fmt.Sprintf("Per-mailbox backup failed: %v", err),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":      backupID,
		"target":  fmt.Sprintf("%s/%s", user, mailbox),
		"path":    destPath,
		"status":  "completed",
		"message": "Per-mailbox backup completed successfully",
	})
}

// ---------------------------------------------------------------------------
// Backup Job Management
// ---------------------------------------------------------------------------

// handleBackupJobList handles GET /api/v1/backup-jobs - list scheduled backup jobs
func (s *Server) handleBackupJobList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	enabledOnly := r.URL.Query().Get("enabled") == "true"

	jobs, err := s.mailDB.ListBackupJobs(enabledOnly)
	if err != nil {
		http.Error(w, "Failed to list backup jobs", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"jobs": jobs,
	})
}

// handleBackupJobCreate handles POST /api/v1/backup-jobs - create a backup job
func (s *Server) handleBackupJobCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var job storage.BackupJob
	if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Generate ID if not provided
	if job.ID == "" {
		job.ID = fmt.Sprintf("job_%d", time.Now().UnixNano())
	}

	err := s.mailDB.CreateBackupJob(&job)
	if err != nil {
		http.Error(w, "Failed to create backup job", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"job":     job,
		"message": "Backup job created",
	})
}

// handleBackupJobGet handles GET /api/v1/backup-jobs/{id} - get a backup job
func (s *Server) handleBackupJobGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobID := extractPathSuffix(r.URL.Path, "/api/v1/backup-jobs/")
	if jobID == "" {
		http.Error(w, "Job ID required", http.StatusBadRequest)
		return
	}

	job, err := s.mailDB.GetBackupJob(jobID)
	if err != nil {
		http.Error(w, "Backup job not found", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(job)
}

// handleBackupJobUpdate handles PUT /api/v1/backup-jobs/{id} - update a backup job
func (s *Server) handleBackupJobUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobID := extractPathSuffix(r.URL.Path, "/api/v1/backup-jobs/")
	if jobID == "" {
		http.Error(w, "Job ID required", http.StatusBadRequest)
		return
	}

	var job storage.BackupJob
	if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	job.ID = jobID
	err := s.mailDB.UpdateBackupJob(&job)
	if err != nil {
		http.Error(w, "Failed to update backup job", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"job":     job,
		"message": "Backup job updated",
	})
}

// handleBackupJobDelete handles DELETE /api/v1/backup-jobs/{id} - delete a backup job
func (s *Server) handleBackupJobDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobID := extractPathSuffix(r.URL.Path, "/api/v1/backup-jobs/")
	if jobID == "" {
		http.Error(w, "Job ID required", http.StatusBadRequest)
		return
	}

	err := s.mailDB.DeleteBackupJob(jobID)
	if err != nil {
		http.Error(w, "Failed to delete backup job", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Backup job deleted",
	})
}

// handleBackupJobRun handles POST /api/v1/backup-jobs/{id}/run - trigger a backup job immediately
func (s *Server) handleBackupJobRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobID := extractPathSuffix(r.URL.Path, "/api/v1/backup-jobs/")
	if jobID == "" {
		http.Error(w, "Job ID required", http.StatusBadRequest)
		return
	}

	job, err := s.mailDB.GetBackupJob(jobID)
	if err != nil {
		http.Error(w, "Backup job not found", http.StatusNotFound)
		return
	}

	if s.backupMgr == nil {
		http.Error(w, "Backup manager not configured", http.StatusServiceUnavailable)
		return
	}

	backupDir := filepath.Join(s.config.DataDir, "backups", "full")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		http.Error(w, "Failed to create backup directory", http.StatusInternalServerError)
		return
	}

	opts := backup.BackupOptions{Encrypted: false}
	var destPath string

	switch job.Type {
	case "per-user":
		backupID := fmt.Sprintf("backup_peruser_%s_%d.tar.gz", job.Target, time.Now().UnixNano())
		destPath = filepath.Join(backupDir, backupID)
		err = s.backupMgr.BackupUser(job.Target, destPath, opts)
	case "per-mailbox":
		parts := strings.SplitN(job.Target, "/", 2)
		if len(parts) != 2 {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":      jobID,
				"status":  "failed",
				"message": "per-mailbox target must be user/mailbox format",
			})
			return
		}
		backupID := fmt.Sprintf("backup_permailbox_%s_%d.tar.gz", job.Target, time.Now().UnixNano())
		destPath = filepath.Join(backupDir, backupID)
		err = s.backupMgr.BackupMailbox(parts[0], parts[1], destPath, opts)
	default:
		backupID := fmt.Sprintf("backup_full_%d.tar.gz", time.Now().UnixNano())
		destPath = filepath.Join(backupDir, backupID)
		err = s.backupMgr.BackupFull(destPath, opts)
	}

	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      jobID,
			"status":  "failed",
			"message": fmt.Sprintf("Backup job failed: %v", err),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":      jobID,
		"status":  "completed",
		"path":    destPath,
		"message": "Backup job completed successfully",
	})
}

// extractPathSuffix extracts the suffix after the given prefix from URL path
func extractPathSuffix(path, prefix string) string {
	suffix := strings.TrimPrefix(path, prefix)
	if suffix == path {
		return "" // prefix not found
	}
	return suffix
}
