package server

import (
	"strings"

	"github.com/umailserver/umailserver/internal/api"
)

// settings is the effective configuration served to the admin Settings page.
// It deliberately omits secrets: keys, passwords, tokens and alert targets.
func (s *Server) settings() api.ServerSettings {
	c := s.config
	listener := func(name string, enabled bool, bind string, port int) api.ListenerSetting {
		return api.ListenerSetting{Name: name, Enabled: enabled && port > 0, Bind: bind, Port: port}
	}
	rbl := make([]string, 0, len(c.Spam.RBLServers))
	for _, z := range c.Spam.RBLServers {
		rbl = append(rbl, maskRBLKey(z))
	}
	minTLS := "1.2"
	if c.TLS.MinVersion == "1.3" {
		minTLS = "1.3"
	}
	return api.ServerSettings{
		ConfigFile: c.Source(),
		Hostname:   c.Server.Hostname,
		DataDir:    c.Server.DataDir,
		Listeners: []api.ListenerSetting{
			listener("SMTP (MX)", c.SMTP.Inbound.Enabled, c.SMTP.Inbound.Bind, c.SMTP.Inbound.Port),
			listener("Submission", c.SMTP.Submission.Enabled, c.SMTP.Submission.Bind, c.SMTP.Submission.Port),
			listener("Submission (TLS)", c.SMTP.SubmissionTLS.Enabled, c.SMTP.SubmissionTLS.Bind, c.SMTP.SubmissionTLS.Port),
			listener("IMAP (TLS)", c.IMAP.Enabled, c.IMAP.Bind, c.IMAP.Port),
			listener("IMAP (STARTTLS)", c.IMAP.Enabled && c.IMAP.STARTTLSPort != c.IMAP.Port, c.IMAP.Bind, c.IMAP.STARTTLSPort),
			listener("POP3", c.POP3.Enabled, c.POP3.Bind, c.POP3.Port),
			listener("ManageSieve", c.ManageSieve.Enabled, c.ManageSieve.Bind, c.ManageSieve.Port),
			listener("HTTP (webmail, API)", true, c.HTTP.Bind, c.HTTP.Port),
			listener("Admin", c.Admin.Enabled, c.Admin.Bind, c.Admin.Port),
			listener("JMAP", c.JMAP.Enabled, c.JMAP.Bind, c.JMAP.Port),
			listener("CalDAV", c.CalDAV.Enabled, c.CalDAV.Bind, c.CalDAV.Port),
			listener("CardDAV", c.CardDAV.Enabled, c.CardDAV.Bind, c.CardDAV.Port),
			listener("Metrics", c.Metrics.Enabled, c.Metrics.Bind, c.Metrics.Port),
			listener("MCP", c.MCP.Enabled, c.MCP.Bind, c.MCP.Port),
		},
		MaxMessageSize: int64(c.SMTP.Inbound.MaxMessageSize),
		MaxRecipients:  c.SMTP.Inbound.MaxRecipients,
		TLS: api.TLSSettings{
			MinVersion: minTLS,
			ACME:       c.TLS.ACME.Enabled,
			ManualCert: c.TLS.CertFile != "" && c.TLS.KeyFile != "",
			ClientAuth: c.TLS.ClientAuth.Enabled,
		},
		Security: api.SecuritySettings{
			MaxLoginAttempts:      c.Security.MaxLoginAttempts,
			LockoutDuration:       c.Security.LockoutDuration.String(),
			SubmissionRequireTLS:  c.SMTP.Submission.RequireTLS,
			IPPerHour:             c.Security.RateLimit.IPPerHour,
			UserPerHour:           c.Security.RateLimit.UserPerHour,
			HTTPRequestsPerMinute: c.Security.RateLimit.HTTPRequestsPerMinute,
			AuditLog:              c.Security.AuditLog.Path != "",
		},
		Spam: api.SpamSettings{
			Enabled:         c.Spam.Enabled,
			RejectThreshold: c.Spam.RejectThreshold,
			JunkThreshold:   c.Spam.JunkThreshold,
			Greylisting:     c.Spam.Enabled && c.Spam.Greylisting.Enabled,
			Bayesian:        c.Spam.Enabled && c.Spam.Bayesian.Enabled,
			AutoTrain:       c.Spam.Enabled && c.Spam.Bayesian.Enabled && c.Spam.Bayesian.AutoTrain,
			RBLServers:      rbl,
			Antivirus:       c.AV.Enabled,
			DMARCReports:    c.DMARC.Enabled && c.DMARC.FromEmail != "",
		},
		Notifications: api.NotificationSettings{
			Alerts:       c.Alert.Enabled,
			AlertEmail:   c.Alert.Enabled && c.Alert.SMTPServer != "" && len(c.Alert.ToAddresses) > 0,
			AlertWebhook: c.Alert.Enabled && c.Alert.WebhookURL != "",
			Push:         c.Push.Enabled,
		},
	}
}

// maskRBLKey hides the account key that Spamhaus Data Query Service zones
// carry as their first label (<key>.zen.dq.spamhaus.net).
func maskRBLKey(zone string) string {
	lower := strings.ToLower(strings.TrimSuffix(zone, "."))
	if !strings.HasSuffix(lower, ".dq.spamhaus.net") && !strings.HasSuffix(lower, ".dq.spamhaus.com") {
		return zone
	}
	labels := strings.Split(zone, ".")
	// key + list + dq + spamhaus + tld
	if len(labels) < 5 {
		return zone
	}
	labels[0] = "***"
	return strings.Join(labels, ".")
}
