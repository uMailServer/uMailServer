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
			Enabled: false,
			Port:    995,
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
			JWTSecret:        "change-me-in-production-32-char-secret",
			RateLimit: RateLimitConfig{
				// Per-IP limits (inbound)
				IPPerMinute:       30,
				IPPerHour:         500,
				IPPerDay:          5000,
				IPConnections:     10,
				// Per-user limits (outbound authenticated)
				UserPerMinute:     60,
				UserPerHour:       1000,
				UserPerDay:        5000,
				UserMaxRecipients: 100,
				// Global limits
				GlobalPerMinute:   10000,
				GlobalPerHour:     100000,
				// Legacy aliases
				SMTPPerMinute:         30,
				SMTPPerHour:           500,
				IMAPConnections:       50,
				HTTPRequestsPerMinute: 120,
			},
		},
		MCP: MCPConfig{
			Enabled:   true,
			Port:      3000,
			AuthToken: "",
			Bind:      "127.0.0.1",
		},
		ManageSieve: ManageSieveConfig{
			Enabled: true,
			Port:    4190,
			Bind:    "0.0.0.0",
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
		Database: DatabaseConfig{
			Path: "/var/lib/umailserver/db",
		},
		Storage: StorageConfig{
			Sync:          true,
			SharedFolders: false,
		},
	}
}
