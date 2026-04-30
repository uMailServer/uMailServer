package config

import (
	"time"
)

// Default configuration values
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Hostname: "localhost",
			DataDir:  "/var/lib/umailserver",
		},
		TLS: TLSConfig{
			ACME: ACMEConfig{
				Enabled:   false,
				Provider:  "letsencrypt",
				Challenge: "http-01",
			},
		},
		SMTP: SMTPConfig{
			Inbound: InboundSMTPConfig{
				Enabled:        true,
				Port:           25,
				Bind:           "0.0.0.0",
				MaxMessageSize: Size(50 * 1024 * 1024), // 50MB
				MaxRecipients:  100,
				MaxConnections: 10000,
				ReadTimeout:    Duration(5 * time.Minute),
				WriteTimeout:   Duration(5 * time.Minute),
			},
			Submission: SubmissionSMTPConfig{
				Enabled:        true,
				Port:           587,
				Bind:           "0.0.0.0",
				RequireAuth:    true,
				RequireTLS:     true,
				MaxConnections: 10000,
			},
			SubmissionTLS: SubmissionTLSConfig{
				Enabled:        true,
				Port:           465,
				Bind:           "0.0.0.0",
				RequireAuth:    true,
				MaxConnections: 10000,
			},
		},
		IMAP: IMAPConfig{
			Enabled:        true,
			Port:           993,
			Bind:           "0.0.0.0",
			STARTTLSPort:   143,
			IdleTimeout:    Duration(30 * time.Minute),
			MaxConnections: 10000,
		},
		POP3: POP3Config{
			Enabled:        false,
			Port:           995,
			Bind:           "0.0.0.0",
			MaxConnections: 10000,
		},
		HTTP: HTTPConfig{
			Enabled:  true,
			Port:     443,
			HTTPPort: 80,
			Bind:     "0.0.0.0",
		},
		Admin: AdminConfig{
			Enabled: true,
			Port:    8443,
			Bind:    "127.0.0.1",
		},
		Spam: SpamConfig{
			Enabled:             true,
			RejectThreshold:     9.0,
			JunkThreshold:       3.0,
			QuarantineThreshold: 6.0,
			Bayesian: BayesianConfig{
				Enabled:   true,
				AutoTrain: true,
			},
			Greylisting: GreylistingConfig{
				Enabled: true,
				Delay:   Duration(5 * time.Minute),
			},
			RBLServers: []string{
				"zen.spamhaus.org",
				"b.barracudacentral.org",
				"bl.spamcop.net",
			},
		},
		AV: AVConfig{
			Enabled: false,
			Addr:    "127.0.0.1:3310",
			Timeout: Duration(30 * time.Second),
			Action:  "reject",
		},
		Security: SecurityConfig{
			MaxLoginAttempts: 5,
			LockoutDuration:  Duration(15 * time.Minute),
			JWTSecret:        "", // Generate secure random at runtime if empty
			AuditLog: AuditLogConfig{
				Path:       "./data/logs/audit.log",
				MaxSizeMB:  10,
				MaxBackups: 5,
				MaxAgeDays: 30,
			},
			RateLimit: RateLimitConfig{
				// Per-IP limits (inbound)
				IPPerMinute:   30,
				IPPerHour:     500,
				IPPerDay:      5000,
				IPConnections: 10,
				// Per-user limits (outbound authenticated)
				UserPerMinute:     60,
				UserPerHour:       1000,
				UserPerDay:        5000,
				UserMaxRecipients: 100,
				// Global limits
				GlobalPerMinute: 10000,
				GlobalPerHour:   100000,
				// Legacy aliases
				SMTPPerMinute:         30,
				SMTPPerHour:           500,
				IMAPConnections:       50,
				HTTPRequestsPerMinute: 120,
			},
		},
		MCP: MCPConfig{
			Enabled:        true,
			Port:           3000,
			AuthToken:      "",
			AdminAuthToken: "",
			Bind:           "127.0.0.1",
		},
		ManageSieve: ManageSieveConfig{
			Enabled: true,
			Port:    4190,
			Bind:    "0.0.0.0",
		},
		CalDAV: CalDAVConfig{
			Enabled: false,
			Port:    8081,
			Bind:    "127.0.0.1",
		},
		CardDAV: CardDAVConfig{
			Enabled: false,
			Port:    8082,
			Bind:    "127.0.0.1",
		},
		JMAP: JMAPConfig{
			Enabled:     false,
			Port:        8083,
			Bind:        "127.0.0.1",
			CorsOrigins: []string{},
		},
		Domains: []DomainConfig{},
		Logging: LoggingConfig{
			Level:      "info",
			Format:     "json",
			Output:     "stdout",
			MaxSizeMB:  100, // 100 MB
			MaxBackups: 10,
			MaxAgeDays: 30,
		},
		Metrics: MetricsConfig{
			Enabled: true,
			Port:    8080,
			Bind:    "127.0.0.1",
			Path:    "/metrics",
		},
		Tracing: TracingConfig{
			Enabled:      false,
			ServiceName:  "umailserver",
			Exporter:     "noop",
			OTLPEndpoint: "localhost:4317",
			Environment:  "production",
			Attributes:   map[string]string{},
			SampleRate:   1.0,
		},
		Database: DatabaseConfig{
			Path: "/var/lib/umailserver/db",
		},
		Storage: StorageConfig{
			Sync:          true,
			SharedFolders: false,
		},
		DMARC: DMARCConfig{
			Enabled:     false,
			OrgName:     "uMailServer",
			FromEmail:   "dmarc-reports@example.com",
			ReportEmail: "",
			Interval:    "24h",
		},
		Alert: AlertConfig{
			Enabled:         false,
			MinInterval:     Duration(5 * time.Minute),
			MaxAlerts:       100,
			DiskThreshold:   85.0,
			MemoryThreshold: 90.0,
			ErrorThreshold:  5.0,
			TLSWarningDays:  7,
			QueueThreshold:  1000,
		},
		Push: PushConfig{
			Enabled: true,
			Subject: "mailto:admin@umailserver.local",
		},
	}
}
