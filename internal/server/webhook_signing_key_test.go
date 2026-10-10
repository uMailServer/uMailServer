package server

// Regression for F5600: the webhook HMAC key must not be the JWT signing
// secret, otherwise any receiver able to verify signatures can mint API tokens.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/config"
	"github.com/umailserver/umailserver/internal/webhook"
)

type webhookSigningDelivery struct {
	body []byte
	sig  string
	has  bool
}

func webhookSigningDeliver(t *testing.T, jwtSecret string) webhookSigningDelivery {
	t.Helper()
	tmp := t.TempDir()
	cfg := &config.Config{
		Server:   config.ServerConfig{Hostname: "test.example.com", DataDir: tmp},
		Database: config.DatabaseConfig{Path: tmp + "/test.db"},
		Logging:  config.LoggingConfig{Level: "error"},
		Security: config.SecurityConfig{JWTSecret: jwtSecret},
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer srv.Stop()

	got := make(chan webhookSigningDelivery, 1)
	recv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_, has := r.Header["X-Webhook-Signature"]
		select {
		case got <- webhookSigningDelivery{body: b, sig: r.Header.Get("X-Webhook-Signature"), has: has}:
		default:
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer recv.Close()

	srv.webhookMgr.SetAllowPrivateIP(true)
	rec := httptest.NewRecorder()
	srv.webhookMgr.HTTPHandler(rec, httptest.NewRequest(http.MethodPost, "/",
		strings.NewReader(`{"url":"`+recv.URL+`","events":["*"]}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create hook: %d", rec.Code)
	}
	srv.webhookMgr.Trigger("mail.received", map[string]string{"to": "a@example.com"})
	select {
	case d := <-got:
		return d
	case <-time.After(10 * time.Second):
		t.Fatal("no webhook delivery")
	}
	return webhookSigningDelivery{}
}

func webhookSigningMAC(key string, body []byte) string {
	h := hmac.New(sha256.New, []byte(key))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// Harness delivers, signature present and well-formed when a secret
// is configured; empty secret means unsigned (never an empty-key HMAC).
func TestWebhookSigning_EmptySecretUnsignedAndSignedWithSecret(t *testing.T) {
	d := webhookSigningDeliver(t, "")
	if d.has {
		t.Fatalf("empty jwt_secret produced a signature header %q", d.sig)
	}
	d = webhookSigningDeliver(t, "test-jwt-secret-0123456789abcdef")
	if len(d.sig) != 64 {
		t.Fatalf("signature missing/malformed: %q", d.sig)
	}
	if d.sig == webhookSigningMAC("", d.body) {
		t.Fatalf("signature uses empty key")
	}
}

// A receiver able to verify the signature holds the JWT signing key.
func TestWebhookSigning_KeyIsNotJWTSecret(t *testing.T) {
	const jwtSecret = "test-jwt-secret-0123456789abcdef"
	d := webhookSigningDeliver(t, jwtSecret)
	reuse := hmac.Equal([]byte(d.sig), []byte(webhookSigningMAC(jwtSecret, d.body)))
	if reuse {
		t.Errorf("F5600: webhook HMAC key == security.jwt_secret (API/JMAP HS256 signing key)")
	}
}

// the delivered signature verifies with the documented derived key, so
// receivers still have a key to verify with.
func TestWebhookSigning_VerifiesWithDerivedKey(t *testing.T) {
	const jwtSecret = "test-jwt-secret-0123456789abcdef"
	d := webhookSigningDeliver(t, jwtSecret)
	if !hmac.Equal([]byte(d.sig), []byte(webhookSigningMAC(webhook.DeriveSigningKey(jwtSecret), d.body))) {
		t.Errorf("F5600: signature does not verify with DeriveSigningKey(jwt_secret)")
	}
}

// empty master stays empty (no public constant key); derivation is
// deterministic (survives restarts) and distinct per master and from it.
func TestWebhookSigning_Derivation(t *testing.T) {
	if k := webhook.DeriveSigningKey(""); k != "" {
		t.Errorf("F5600: empty master derived non-empty public key %q", k)
	}
	a1, a2 := webhook.DeriveSigningKey("a"), webhook.DeriveSigningKey("a")
	b := webhook.DeriveSigningKey("b")
	if a1 != a2 || a1 == b || a1 == "a" || len(a1) != 64 {
		t.Errorf("F5600: derivation a1=%q a2=%q b=%q", a1, a2, b)
	}
}
