package push

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type captureRT struct{ req chan *http.Request }

func (c *captureRT) RoundTrip(req *http.Request) (*http.Response, error) {
	c.req <- req
	return &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}, Request: req}, nil
}

// Pins the wire contract delegated to webpush-go: VAPID JWT exp <= 24h and
// aud == push service origin, aes128gcm content coding, TTL header (F6638).
func TestWireContractVAPIDAndEncoding_F6638(t *testing.T) {
	rec := &captureRT{req: make(chan *http.Request, 1)}
	old := pushHTTPClient.Transport
	pushHTTPClient.Transport = rec
	defer func() { pushHTTPClient.Transport = old }()

	key, _ := ecdh.P256().GenerateKey(rand.Reader)
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	svc, err := NewService(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	sub := &Subscription{ID: "s", UserID: "u", Endpoint: "https://push.example.invalid:8443/wpush/v2/abc",
		P256dh: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
		Auth:   base64.RawURLEncoding.EncodeToString(auth)}
	if err := svc.SendNotification(sub, &Notification{Title: "t"}); err != nil {
		t.Fatal(err)
	}
	req := <-rec.req
	if req.Header.Get("Content-Encoding") != "aes128gcm" {
		t.Fatalf("content-encoding %q", req.Header.Get("Content-Encoding"))
	}
	if req.Header.Get("TTL") == "" {
		t.Fatal("missing TTL")
	}
	authz := req.Header.Get("Authorization")
	var jwt string
	for _, p := range strings.Split(strings.TrimPrefix(authz, "vapid "), ",") {
		if p = strings.TrimSpace(p); strings.HasPrefix(p, "t=") {
			jwt = p[2:]
		}
	}
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("authorization %q", authz)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims struct {
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Aud != "https://push.example.invalid:8443" {
		t.Fatalf("aud=%q", claims.Aud)
	}
	if d := time.Until(time.Unix(claims.Exp, 0)); d <= 0 || d > 24*time.Hour {
		t.Fatalf("exp in %v, must be within 24h", d)
	}
}

// F6637: a rejected Subscribe (cap reached) must not delete another user's
// subscription that shares the endpoint.
func TestSubscribeAtCapDoesNotEvictOtherUsersEndpoint_F6637(t *testing.T) {
	svc, err := NewService(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxSubscriptionsPerUser; i++ {
		if err := svc.Subscribe("a", &Subscription{Endpoint: fmt.Sprintf("https://p.example.invalid/a%d", i), P256dh: testP256dh, Auth: testAuth}); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.Subscribe("b", &Subscription{ID: "bsub", Endpoint: "https://p.example.invalid/shared", P256dh: testP256dh, Auth: testAuth}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Subscribe("a", &Subscription{Endpoint: "https://p.example.invalid/shared", P256dh: testP256dh, Auth: testAuth}); err == nil {
		t.Fatal("expected cap rejection")
	}
	if len(svc.GetUserSubscriptions("b")) != 1 {
		t.Fatal("rejected Subscribe destroyed another user's subscription")
	}
	if len(svc.GetUserSubscriptions("a")) != MaxSubscriptionsPerUser {
		t.Fatal("user a count changed")
	}
	// Re-registering an own endpoint at cap is a replace, not growth.
	if err := svc.Subscribe("a", &Subscription{Endpoint: "https://p.example.invalid/a0", P256dh: testP256dh, Auth: testAuth}); err != nil {
		t.Fatalf("replace at cap rejected: %v", err)
	}
}

// Concurrent Subscribe never exceeds the per-user cap.
func TestConcurrentSubscribeRespectsCap_F6637(t *testing.T) {
	svc, err := NewService(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = svc.Subscribe("u", &Subscription{Endpoint: fmt.Sprintf("https://p.example.invalid/%d", i), P256dh: testP256dh, Auth: testAuth})
		}(i)
	}
	wg.Wait()
	if n := len(svc.GetUserSubscriptions("u")); n != MaxSubscriptionsPerUser {
		t.Fatalf("subs=%d, want %d", n, MaxSubscriptionsPerUser)
	}
}
