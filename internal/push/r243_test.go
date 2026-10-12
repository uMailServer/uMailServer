package push

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

// F7202: Subscribe validates p256dh and auth key shapes.
func TestF7202_SubscribeValidatesKeys(t *testing.T) {
	s, err := NewService(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	k, _ := ecdh.P256().GenerateKey(rand.Reader)
	goodP := base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes())
	goodA := base64.RawURLEncoding.EncodeToString(make([]byte, 16))
	short := base64.RawURLEncoding.EncodeToString(make([]byte, 8))
	bad := []struct{ p, a string }{
		{"test-p256dh-key", goodA},
		{goodP, "test-auth-secret"},
		{"", goodA},
		{goodP, ""},
		{short, goodA},
		{goodP, short},
		{base64.RawURLEncoding.EncodeToString(make([]byte, 65)), goodA}, // not on curve
	}
	for i, c := range bad {
		if err := s.Subscribe("u", &Subscription{Endpoint: "https://push.example/x", P256dh: c.p, Auth: c.a}); err == nil {
			t.Errorf("case %d: invalid keys accepted", i)
		}
	}
	if err := s.Subscribe("u", &Subscription{Endpoint: "https://push.example/x", P256dh: goodP, Auth: goodA}); err != nil {
		t.Errorf("valid keys rejected: %v", err)
	}
}

// Valid Web Push keys (RFC 8291 test vector) for tests that need a
// Subscribe-acceptable subscription.
const (
	testP256dh = "BNcRdreALRFXTkOOUHK1EtK2wtaz5Ry4YfYCA_0QTpQtUbVlUls0VJXg7A8u-Ts1XbjhazAkj7I99e8QcYP7DkM"
	testAuth   = "tBHItJI5svbpez7KI4CCXg"
)
