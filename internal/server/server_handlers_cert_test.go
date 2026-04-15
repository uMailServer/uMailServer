package server

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"testing"
)

func TestAuthenticateClientCert_NilCert(t *testing.T) {
	srv := helperServer(t)

	email, ok := srv.authenticateClientCert(nil)

	if ok {
		t.Error("expected not authenticated with nil cert")
	}
	if email != "" {
		t.Error("expected empty email with nil cert")
	}
}

// TestAuthenticateClientCert_NoEmail tests with certificate that has no email
func TestAuthenticateClientCert_NoEmail(t *testing.T) {
	srv := helperServer(t)

	cert := &x509.Certificate{
		Subject: pkix.Name{
			CommonName: "notanemail",
		},
		EmailAddresses: []string{},
	}

	email, ok := srv.authenticateClientCert(cert)

	if ok {
		t.Error("expected not authenticated without email")
	}
	if email != "" {
		t.Error("expected empty email without email")
	}
}

// TestAuthenticateClientCert_WithEmail tests with certificate that has email
func TestAuthenticateClientCert_WithEmail(t *testing.T) {
	srv := helperServer(t)

	// Create domain and account first
	helperCreateDomain(t, srv, "example.com", true)
	helperCreateAccount(t, srv, "user", "example.com", true, 1000000, 0)

	cert := &x509.Certificate{
		Subject: pkix.Name{
			CommonName: "user@example.com",
		},
		EmailAddresses: []string{"user@example.com"},
	}

	email, ok := srv.authenticateClientCert(cert)

	if !ok {
		t.Error("expected authenticated with valid email")
	}
	if email != "user@example.com" {
		t.Errorf("expected email user@example.com, got %s", email)
	}
}

// TestAuthenticateClientCert_CommonNameAsEmail tests using CommonName as email
func TestAuthenticateClientCert_CommonNameAsEmail(t *testing.T) {
	srv := helperServer(t)

	helperCreateDomain(t, srv, "example.com", true)
	helperCreateAccount(t, srv, "cnuser", "example.com", true, 1000000, 0)

	cert := &x509.Certificate{
		Subject: pkix.Name{
			CommonName: "cnuser@example.com",
		},
		EmailAddresses: []string{}, // No email addresses
	}

	email, ok := srv.authenticateClientCert(cert)

	if !ok {
		t.Error("expected authenticated with CommonName as email")
	}
	if email != "cnuser@example.com" {
		t.Errorf("expected email cnuser@example.com, got %s", email)
	}
}

// TestAuthenticateClientCert_NonExistentAccount tests with non-existent account
func TestAuthenticateClientCert_NonExistentAccount(t *testing.T) {
	srv := helperServer(t)

	// Don't create the account
	cert := &x509.Certificate{
		Subject: pkix.Name{
			CommonName: "nonexistent@example.com",
		},
		EmailAddresses: []string{"nonexistent@example.com"},
	}

	email, ok := srv.authenticateClientCert(cert)

	if ok {
		t.Error("expected not authenticated for non-existent account")
	}
	if email != "" {
		t.Error("expected empty email for non-existent account")
	}
}

// TestAuthenticateClientCert_InactiveAccount tests with inactive account
func TestAuthenticateClientCert_InactiveAccount(t *testing.T) {
	srv := helperServer(t)

	helperCreateDomain(t, srv, "example.com", true)
	helperCreateAccount(t, srv, "inactive", "example.com", false, 1000000, 0)

	cert := &x509.Certificate{
		Subject: pkix.Name{
			CommonName: "inactive@example.com",
		},
		EmailAddresses: []string{"inactive@example.com"},
	}

	email, ok := srv.authenticateClientCert(cert)

	if ok {
		t.Error("expected not authenticated for inactive account")
	}
	if email != "" {
		t.Error("expected empty email for inactive account")
	}
}

// TestAuthenticateClientCert_MultipleEmailAddresses tests with multiple email addresses
func TestAuthenticateClientCert_MultipleEmailAddresses(t *testing.T) {
	srv := helperServer(t)

	helperCreateDomain(t, srv, "example.com", true)
	helperCreateAccount(t, srv, "first", "example.com", true, 1000000, 0)

	cert := &x509.Certificate{
		Subject: pkix.Name{
			CommonName: "second@example.com",
		},
		EmailAddresses: []string{"first@example.com", "second@example.com"}, // Multiple emails
	}

	email, ok := srv.authenticateClientCert(cert)

	if !ok {
		t.Error("expected authenticated with first email")
	}
	if email != "first@example.com" {
		t.Errorf("expected first email first@example.com, got %s", email)
	}
}
