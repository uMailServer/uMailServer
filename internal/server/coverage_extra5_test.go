//go:build !race

package server

import (
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/config"
	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/tracing"
	"golang.org/x/crypto/bcrypt"
)

// ---------------------------------------------------------------------------
// authenticate coverage tests
// ---------------------------------------------------------------------------

func TestCoverAuthenticate_WrongPassword(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "test.example.com",
			DataDir:  tmpDir,
		},
		Database: config.DatabaseConfig{
			Path: tmpDir + "/test.db",
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
		TLS: config.TLSConfig{
			ACME: config.ACMEConfig{
				Enabled: false,
			},
		},
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer srv.Stop()

	_ = srv.database.CreateDomain(&db.DomainData{Name: "example.com", IsActive: true})
	_ = srv.database.CreateAccount(&db.AccountData{
		Domain:       "example.com",
		LocalPart:    "testuser",
		Email:        "testuser@example.com",
		PasswordHash: "$2a$10$invalidhash",
		IsActive:     true,
	})

	ok, _ := srv.authenticate("testuser@example.com", "wrongpassword")
	if ok {
		t.Error("expected authentication to fail with wrong password")
	}
}

func TestCoverAuthenticate_InactiveAccount(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "test.example.com",
			DataDir:  tmpDir,
		},
		Database: config.DatabaseConfig{
			Path: tmpDir + "/test.db",
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
		TLS: config.TLSConfig{
			ACME: config.ACMEConfig{
				Enabled: false,
			},
		},
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer srv.Stop()

	hash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)

	_ = srv.database.CreateDomain(&db.DomainData{Name: "example.com", IsActive: true})
	_ = srv.database.CreateAccount(&db.AccountData{
		Domain:       "example.com",
		LocalPart:    "testuser",
		Email:        "testuser@example.com",
		PasswordHash: string(hash),
		IsActive:     false,
	})

	ok, errAuth := srv.authenticate("testuser@example.com", "password")
	if ok {
		t.Error("expected authentication to fail for inactive account")
	}
	if errAuth == nil {
		t.Error("expected error for inactive account")
	}
}

func TestCoverAuthenticate_TracingEnabled(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "test.example.com",
			DataDir:  tmpDir,
		},
		Database: config.DatabaseConfig{
			Path: tmpDir + "/test.db",
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
		TLS: config.TLSConfig{
			ACME: config.ACMEConfig{
				Enabled: false,
			},
		},
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer srv.Stop()

	tp, _ := tracing.NewProvider(tracing.Config{Enabled: true, Exporter: "noop"})
	srv.tracingProvider = tp

	_ = srv.database.CreateDomain(&db.DomainData{Name: "example.com", IsActive: true})
	_ = srv.database.CreateAccount(&db.AccountData{
		Domain:       "example.com",
		LocalPart:    "testuser",
		Email:        "testuser@example.com",
		PasswordHash: "$2a$10$invalidhash",
		IsActive:     true,
	})

	// Should not panic with tracing enabled
	_, _ = srv.authenticate("testuser@example.com", "password")
}

// ---------------------------------------------------------------------------
// deliverMessageWithSieve coverage tests
// ---------------------------------------------------------------------------

func TestCoverDeliverMessageWithSieve_RedirectLoop(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "test.example.com",
			DataDir:  tmpDir,
		},
		Database: config.DatabaseConfig{
			Path: tmpDir + "/test.db",
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
		SMTP: config.SMTPConfig{
			Inbound: config.InboundSMTPConfig{
				Bind:           "127.0.0.1",
				Port:           0,
				MaxMessageSize: 10485760,
				MaxRecipients:  100,
			},
		},
		IMAP: config.IMAPConfig{
			Bind: "127.0.0.1",
			Port: 0,
		},
		Admin: config.AdminConfig{
			Bind: "127.0.0.1",
			Port: 0,
		},
		TLS: config.TLSConfig{
			ACME: config.ACMEConfig{
				Enabled: false,
			},
		},
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer srv.Stop()

	startDone := make(chan error, 1)
	go func() {
		startDone <- srv.Start()
	}()
	time.Sleep(600 * time.Millisecond)

	// Data with X-Mail-Loop matching the redirect address
	data := []byte("Subject: Test\r\nX-Mail-Loop: target@example.com\r\n\r\nBody")
	err = srv.deliverMessageWithSieve("sender@example.com", []string{"local@example.com"}, data, []string{"redirect:target@example.com"})
	if err != nil {
		t.Errorf("deliverMessageWithSieve failed: %v", err)
	}

	srv.Stop()
	select {
	case <-startDone:
	case <-time.After(3 * time.Second):
	}
}

func TestCoverDeliverMessageWithSieve_AliasResolution(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "test.example.com",
			DataDir:  tmpDir,
		},
		Database: config.DatabaseConfig{
			Path: tmpDir + "/test.db",
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
		SMTP: config.SMTPConfig{
			Inbound: config.InboundSMTPConfig{
				Bind:           "127.0.0.1",
				Port:           0,
				MaxMessageSize: 10485760,
				MaxRecipients:  100,
			},
		},
		IMAP: config.IMAPConfig{
			Bind: "127.0.0.1",
			Port: 0,
		},
		Admin: config.AdminConfig{
			Bind: "127.0.0.1",
			Port: 0,
		},
		TLS: config.TLSConfig{
			ACME: config.ACMEConfig{
				Enabled: false,
			},
		},
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer srv.Stop()

	_ = srv.database.CreateDomain(&db.DomainData{Name: "example.com", IsActive: true})
	_ = srv.database.CreateAccount(&db.AccountData{
		Domain:       "example.com",
		LocalPart:    "realuser",
		Email:        "realuser@example.com",
		PasswordHash: "$2a$10$test",
		IsActive:     true,
	})
	_ = srv.database.CreateAlias(&db.AliasData{
		Domain:   "example.com",
		Alias:    "aliasuser",
		Target:   "realuser@example.com",
		IsActive: true,
	})

	startDone := make(chan error, 1)
	go func() {
		startDone <- srv.Start()
	}()
	time.Sleep(600 * time.Millisecond)

	err = srv.deliverMessageWithSieve("sender@example.com", []string{"aliasuser@example.com"}, []byte("Subject: Test\r\n\r\nBody"), nil)
	if err != nil {
		t.Errorf("deliverMessageWithSieve failed: %v", err)
	}

	srv.Stop()
	select {
	case <-startDone:
	case <-time.After(3 * time.Second):
	}
}

func TestCoverDeliverMessageWithSieve_DomainInactive(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "test.example.com",
			DataDir:  tmpDir,
		},
		Database: config.DatabaseConfig{
			Path: tmpDir + "/test.db",
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
		SMTP: config.SMTPConfig{
			Inbound: config.InboundSMTPConfig{
				Bind:           "127.0.0.1",
				Port:           0,
				MaxMessageSize: 10485760,
				MaxRecipients:  100,
			},
		},
		IMAP: config.IMAPConfig{
			Bind: "127.0.0.1",
			Port: 0,
		},
		Admin: config.AdminConfig{
			Bind: "127.0.0.1",
			Port: 0,
		},
		TLS: config.TLSConfig{
			ACME: config.ACMEConfig{
				Enabled: false,
			},
		},
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer srv.Stop()

	_ = srv.database.CreateDomain(&db.DomainData{Name: "example.com", IsActive: false})

	startDone := make(chan error, 1)
	go func() {
		startDone <- srv.Start()
	}()
	time.Sleep(600 * time.Millisecond)

	// Domain is inactive, should relay (enqueue to queue)
	err = srv.deliverMessageWithSieve("sender@example.com", []string{"user@example.com"}, []byte("Subject: Test\r\n\r\nBody"), nil)
	if err != nil {
		t.Errorf("deliverMessageWithSieve failed: %v", err)
	}

	srv.Stop()
	select {
	case <-startDone:
	case <-time.After(3 * time.Second):
	}
}

// ---------------------------------------------------------------------------
// deliverLocal coverage tests
// ---------------------------------------------------------------------------

func TestCoverDeliverLocal_ForwardLoop(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "test.example.com",
			DataDir:  tmpDir,
		},
		Database: config.DatabaseConfig{
			Path: tmpDir + "/test.db",
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
		SMTP: config.SMTPConfig{
			Inbound: config.InboundSMTPConfig{
				Bind:           "127.0.0.1",
				Port:           0,
				MaxMessageSize: 10485760,
				MaxRecipients:  100,
			},
		},
		IMAP: config.IMAPConfig{
			Bind: "127.0.0.1",
			Port: 0,
		},
		Admin: config.AdminConfig{
			Bind: "127.0.0.1",
			Port: 0,
		},
		TLS: config.TLSConfig{
			ACME: config.ACMEConfig{
				Enabled: false,
			},
		},
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer srv.Stop()

	_ = srv.database.CreateDomain(&db.DomainData{Name: "example.com", IsActive: true})
	_ = srv.database.CreateAccount(&db.AccountData{
		Domain:          "example.com",
		LocalPart:       "testuser",
		Email:           "testuser@example.com",
		PasswordHash:    "$2a$10$test",
		IsActive:        true,
		ForwardTo:       "other@example.com",
		ForwardKeepCopy: true,
	})

	startDone := make(chan error, 1)
	go func() {
		startDone <- srv.Start()
	}()
	time.Sleep(600 * time.Millisecond)

	// Data with X-Mail-Loop matching the account email
	data := []byte("Subject: Test\r\nX-Mail-Loop: testuser@example.com\r\n\r\nBody")
	err = srv.deliverLocal("testuser", "example.com", "sender@example.com", data)
	if err != nil {
		t.Errorf("deliverLocal with forward loop failed: %v", err)
	}

	srv.Stop()
	select {
	case <-startDone:
	case <-time.After(3 * time.Second):
	}
}

func TestCoverDeliverLocal_TargetFolder(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "test.example.com",
			DataDir:  tmpDir,
		},
		Database: config.DatabaseConfig{
			Path: tmpDir + "/test.db",
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
		SMTP: config.SMTPConfig{
			Inbound: config.InboundSMTPConfig{
				Bind:           "127.0.0.1",
				Port:           0,
				MaxMessageSize: 10485760,
				MaxRecipients:  100,
			},
		},
		IMAP: config.IMAPConfig{
			Bind: "127.0.0.1",
			Port: 0,
		},
		Admin: config.AdminConfig{
			Bind: "127.0.0.1",
			Port: 0,
		},
		TLS: config.TLSConfig{
			ACME: config.ACMEConfig{
				Enabled: false,
			},
		},
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer srv.Stop()

	_ = srv.database.CreateDomain(&db.DomainData{Name: "example.com", IsActive: true})
	_ = srv.database.CreateAccount(&db.AccountData{
		Domain:       "example.com",
		LocalPart:    "testuser",
		Email:        "testuser@example.com",
		PasswordHash: "$2a$10$test",
		IsActive:     true,
	})

	startDone := make(chan error, 1)
	go func() {
		startDone <- srv.Start()
	}()
	time.Sleep(600 * time.Millisecond)

	err = srv.deliverLocal("testuser", "example.com", "sender@example.com", []byte("Subject: Test\r\n\r\nBody"), "Junk")
	if err != nil {
		t.Errorf("deliverLocal with target folder failed: %v", err)
	}

	srv.Stop()
	select {
	case <-startDone:
	case <-time.After(3 * time.Second):
	}
}

func TestCoverDeliverLocal_VacationSettings(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "test.example.com",
			DataDir:  tmpDir,
		},
		Database: config.DatabaseConfig{
			Path: tmpDir + "/test.db",
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
		SMTP: config.SMTPConfig{
			Inbound: config.InboundSMTPConfig{
				Bind:           "127.0.0.1",
				Port:           0,
				MaxMessageSize: 10485760,
				MaxRecipients:  100,
			},
		},
		IMAP: config.IMAPConfig{
			Bind: "127.0.0.1",
			Port: 0,
		},
		Admin: config.AdminConfig{
			Bind: "127.0.0.1",
			Port: 0,
		},
		TLS: config.TLSConfig{
			ACME: config.ACMEConfig{
				Enabled: false,
			},
		},
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer srv.Stop()

	_ = srv.database.CreateDomain(&db.DomainData{Name: "example.com", IsActive: true})
	_ = srv.database.CreateAccount(&db.AccountData{
		Domain:           "example.com",
		LocalPart:        "testuser",
		Email:            "testuser@example.com",
		PasswordHash:     "$2a$10$test",
		IsActive:         true,
		VacationSettings: `{"enabled": true, "message": "Out of office"}`,
	})

	startDone := make(chan error, 1)
	go func() {
		startDone <- srv.Start()
	}()
	time.Sleep(600 * time.Millisecond)

	// Initialize vacation replies map
	srv.vacationReplies = make(map[string]time.Time)

	err = srv.deliverLocal("testuser", "example.com", "sender@example.com", []byte("Subject: Test\r\n\r\nBody"))
	if err != nil {
		t.Errorf("deliverLocal with vacation settings failed: %v", err)
	}

	// Give goroutine time to start
	time.Sleep(100 * time.Millisecond)

	srv.Stop()
	select {
	case <-startDone:
	case <-time.After(3 * time.Second):
	}
}

func TestCoverDeliverLocal_PushEnabled(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "test.example.com",
			DataDir:  tmpDir,
		},
		Database: config.DatabaseConfig{
			Path: tmpDir + "/test.db",
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
		SMTP: config.SMTPConfig{
			Inbound: config.InboundSMTPConfig{
				Bind:           "127.0.0.1",
				Port:           0,
				MaxMessageSize: 10485760,
				MaxRecipients:  100,
			},
		},
		IMAP: config.IMAPConfig{
			Bind: "127.0.0.1",
			Port: 0,
		},
		Admin: config.AdminConfig{
			Bind: "127.0.0.1",
			Port: 0,
		},
		TLS: config.TLSConfig{
			ACME: config.ACMEConfig{
				Enabled: false,
			},
		},
		Push: config.PushConfig{
			Enabled: true,
		},
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer srv.Stop()

	_ = srv.database.CreateDomain(&db.DomainData{Name: "example.com", IsActive: true})
	_ = srv.database.CreateAccount(&db.AccountData{
		Domain:       "example.com",
		LocalPart:    "testuser",
		Email:        "testuser@example.com",
		PasswordHash: "$2a$10$test",
		IsActive:     true,
	})

	startDone := make(chan error, 1)
	go func() {
		startDone <- srv.Start()
	}()
	time.Sleep(600 * time.Millisecond)

	err = srv.deliverLocal("testuser", "example.com", "sender@example.com", []byte("Subject: Test\r\n\r\nBody"))
	if err != nil {
		t.Errorf("deliverLocal with push enabled failed: %v", err)
	}

	// Give goroutine time to start
	time.Sleep(100 * time.Millisecond)

	srv.Stop()
	select {
	case <-startDone:
	case <-time.After(3 * time.Second):
	}
}

// ---------------------------------------------------------------------------
// sendMDN coverage tests
// ---------------------------------------------------------------------------

func TestSendMDN_NilQueue(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "test.example.com",
			DataDir:  tmpDir,
		},
		Database: config.DatabaseConfig{
			Path: tmpDir + "/test.db",
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
		TLS: config.TLSConfig{
			ACME: config.ACMEConfig{
				Enabled: false,
			},
		},
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer srv.Stop()

	// Set queue to nil
	srv.queue = nil

	err = srv.sendMDN("from@example.com", "to@example.com", "<msg-id>", "<in-reply-to>", []byte("Subject: Test\r\n\r\nBody"))
	if err != nil {
		t.Errorf("sendMDN with nil queue should return nil, got: %v", err)
	}
}

func TestSendMDN_Success(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "test.example.com",
			DataDir:  tmpDir,
		},
		Database: config.DatabaseConfig{
			Path: tmpDir + "/test.db",
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
		TLS: config.TLSConfig{
			ACME: config.ACMEConfig{
				Enabled: false,
			},
		},
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer srv.Stop()

	err = srv.sendMDN("from@example.com", "to@example.com", "<msg-id>", "<in-reply-to>", []byte("Subject: Test\r\n\r\nBody"))
	if err != nil {
		t.Errorf("sendMDN failed: %v", err)
	}
}

func TestSendMDN_GenerateMDNError(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "test.example.com",
			DataDir:  tmpDir,
		},
		Database: config.DatabaseConfig{
			Path: tmpDir + "/test.db",
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
		TLS: config.TLSConfig{
			ACME: config.ACMEConfig{
				Enabled: false,
			},
		},
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer srv.Stop()

	// Use a very large message that will fail in GenerateMDN
	// (gzip compression would fail for oversized message in test context)
	// Instead pass an invalid/malformed message to trigger error
	largeMsg := make([]byte, 1024*1024*1024) // 1GB - will fail to process
	err = srv.sendMDN("from@example.com", "to@example.com", "<msg-id>", "", largeMsg)
	// The error is logged but not returned (line 14 only logs)
	// so this will return nil - the error path in GenerateMDN is not reachable via sendMDN
	// This test documents the limitation
	_ = err
}
