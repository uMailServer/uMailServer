package api

import "net/http"

// ServerSettings is the effective configuration shown by the admin Settings
// page. It is read-only: settings change in the config file and take effect
// on restart. It carries no secrets.
type ServerSettings struct {
	ConfigFile     string               `json:"config_file"` // "" = built-in defaults
	Hostname       string               `json:"hostname"`
	DataDir        string               `json:"data_dir"`
	Listeners      []ListenerSetting    `json:"listeners"`
	MaxMessageSize int64                `json:"max_message_size"` // bytes
	MaxRecipients  int                  `json:"max_recipients"`
	TLS            TLSSettings          `json:"tls"`
	Security       SecuritySettings     `json:"security"`
	Spam           SpamSettings         `json:"spam"`
	Notifications  NotificationSettings `json:"notifications"`
}

// ListenerSetting is one protocol listener.
type ListenerSetting struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Bind    string `json:"bind"`
	Port    int    `json:"port"`
}

// TLSSettings summarizes certificate and TLS settings.
type TLSSettings struct {
	MinVersion string `json:"min_version"`
	ACME       bool   `json:"acme"`
	ManualCert bool   `json:"manual_cert"`
	ClientAuth bool   `json:"client_auth"`
}

// SecuritySettings summarizes login protection and rate limits.
type SecuritySettings struct {
	MaxLoginAttempts      int    `json:"max_login_attempts"`
	LockoutDuration       string `json:"lockout_duration"`
	SubmissionRequireTLS  bool   `json:"submission_require_tls"`
	IPPerHour             int    `json:"ip_per_hour"`
	UserPerHour           int    `json:"user_per_hour"`
	HTTPRequestsPerMinute int    `json:"http_requests_per_minute"`
	AuditLog              bool   `json:"audit_log"`
}

// SpamSettings summarizes inbound filtering.
type SpamSettings struct {
	Enabled         bool     `json:"enabled"`
	RejectThreshold float64  `json:"reject_threshold"`
	JunkThreshold   float64  `json:"junk_threshold"`
	Greylisting     bool     `json:"greylisting"`
	Bayesian        bool     `json:"bayesian"`
	AutoTrain       bool     `json:"auto_train"`
	RBLServers      []string `json:"rbl_servers"`
	Antivirus       bool     `json:"antivirus"`
	DMARCReports    bool     `json:"dmarc_reports"`
}

// NotificationSettings summarizes operator alerts and user push.
type NotificationSettings struct {
	Alerts       bool `json:"alerts"`
	AlertEmail   bool `json:"alert_email"`
	AlertWebhook bool `json:"alert_webhook"`
	Push         bool `json:"push"`
}

// SetSettingsFunc installs the source of GET /api/v1/admin/settings.
func (s *Server) SetSettingsFunc(f func() ServerSettings) {
	s.settingsFunc = f
}

// handleAdminSettings serves the effective server settings (admin only).
func (s *Server) handleAdminSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.settingsFunc == nil {
		s.sendError(w, http.StatusServiceUnavailable, "settings not available")
		return
	}
	s.sendJSON(w, http.StatusOK, s.settingsFunc())
}
