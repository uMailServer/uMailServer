package push

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type fakeTransport struct {
	mu     sync.Mutex
	status int
	reqs   []*http.Request
}

func (f *fakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	f.mu.Unlock()
	return &http.Response{StatusCode: f.status, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}, Request: req}, nil
}

func useFake(t *testing.T, status int) *fakeTransport {
	t.Helper()
	f := &fakeTransport{status: status}
	old := pushHTTPClient.Transport
	pushHTTPClient.Transport = f
	t.Cleanup(func() { pushHTTPClient.Transport = old })
	return f
}

func validKeys(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	return base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), base64.RawURLEncoding.EncodeToString(auth)
}

func newSvc(t *testing.T) *Service {
	t.Helper()
	s, err := NewService(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// F5760: endpoints must be https to a public host.
func TestF5760_EndpointValidation(t *testing.T) {
	svc := newSvc(t)
	bad := []string{
		"http://push.example.com/x", "https://127.0.0.1/x", "https://localhost/x",
		"https://169.254.169.254/latest", "https://10.0.0.5/x", "https://192.168.1.1/",
		"https://[::1]/x", "https://[::ffff:127.0.0.1]/x", "https://user:pw@push.example.com/x",
		"https://100.64.0.1/x", "ftp://x.example.com", "https:///x",
	}
	for _, ep := range bad {
		if err := svc.Subscribe("u", &Subscription{Endpoint: ep}); err == nil {
			t.Errorf("Subscribe accepted %q", ep)
		}
		if err := svc.SendNotification(&Subscription{ID: "x", UserID: "u", Endpoint: ep}, &Notification{Title: "t"}); err == nil {
			t.Errorf("SendNotification accepted %q", ep)
		}
	}
	if err := svc.Subscribe("u", &Subscription{Endpoint: "https://fcm.googleapis.com/fcm/send/a"}); err != nil {
		t.Errorf("valid endpoint rejected: %v", err)
	}
}

// F5760: the dialer blocks private addresses (covers DNS rebinding / redirects).
func TestF5760_DialerBlocksLoopback(t *testing.T) {
	c := &http.Client{Transport: newPushTransport(), Timeout: 3 * time.Second}
	if _, err := c.Get("https://127.0.0.1:1/"); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected blocked dial, got %v", err)
	}
}

// F5761/F5762: IDs are validated and cannot hijack another user's record.
func TestF5761_F5762_SubscriptionID(t *testing.T) {
	svc := newSvc(t)
	if err := svc.Subscribe("u", &Subscription{ID: "../evil", Endpoint: "https://a.example.com/1"}); err == nil {
		t.Error("path traversal ID accepted")
	}
	if err := svc.Subscribe("alice", &Subscription{ID: "s1", Endpoint: "https://a.example.com/1"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Subscribe("bob", &Subscription{ID: "s1", Endpoint: "https://a.example.com/2"}); err == nil {
		t.Error("bob overwrote alice's subscription ID")
	}
	if got := svc.GetUserSubscriptions("alice"); len(got) != 1 || got[0].Endpoint != "https://a.example.com/1" {
		t.Errorf("alice's subscription damaged: %+v", got)
	}
	// Same-user re-subscribe with same ID must not duplicate userSubs.
	if err := svc.Subscribe("alice", &Subscription{ID: "s1", Endpoint: "https://a.example.com/3"}); err != nil {
		t.Fatal(err)
	}
	if got := svc.GetUserSubscriptions("alice"); len(got) != 1 {
		t.Errorf("duplicate entries: %d", len(got))
	}
}

// F5763: failed persistence leaves no in-memory record.
func TestF5763_SaveFailureRollsBack(t *testing.T) {
	dir := t.TempDir()
	svc, _ := NewService(dir, nil)
	svc.dataDir = filepath.Join(dir, "missing", "nested")
	if err := svc.Subscribe("u", &Subscription{Endpoint: "https://a.example.com/1"}); err == nil {
		t.Fatal("expected save error")
	}
	if n := len(svc.GetUserSubscriptions("u")); n != 0 {
		t.Errorf("phantom subscription kept: %d", n)
	}
}

// F5764: returned subscriptions are copies (race with UpdateDeviceInfo).
func TestF5764_NoRaceOnReturnedSubscriptions(t *testing.T) {
	svc := newSvc(t)
	_ = svc.Subscribe("u", &Subscription{ID: "s", Endpoint: "https://a.example.com/1"})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = svc.UpdateDeviceInfo("u", "s", DeviceInfo{Name: "n"})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			for _, s := range svc.GetUserSubscriptions("u") {
				_ = s.DeviceInfo.Name
				_ = s.UpdatedAt
			}
		}
	}()
	wg.Wait()
}

// F5765: oversized mail subjects still produce a deliverable payload.
func TestF5765_OversizedPayload(t *testing.T) {
	fake := useFake(t, 201)
	svc := newSvc(t)
	p, a := validKeys(t)
	_ = svc.Subscribe("u", &Subscription{ID: "s", Endpoint: "https://a.example.com/1", P256dh: p, Auth: a})
	if err := svc.SendNewMailNotification("u", strings.Repeat("f", 3000), strings.Repeat("é", 5000), ""); err != nil {
		t.Fatalf("oversized notification failed: %v", err)
	}
	if len(fake.reqs) != 1 {
		t.Fatalf("requests=%d", len(fake.reqs))
	}
}

// F5766: VAPID sub claim must be a single mailto: / https: URI; check aud/exp too.
func TestF5766_VAPIDClaims(t *testing.T) {
	fake := useFake(t, 201)
	svc := newSvc(t)
	p, a := validKeys(t)
	sub := &Subscription{ID: "s", UserID: "u", Endpoint: "https://push.example.com/x/y", P256dh: p, Auth: a}
	if err := svc.SendNotification(sub, &Notification{Title: "t"}); err != nil {
		t.Fatal(err)
	}
	auth := fake.reqs[0].Header.Get("Authorization")
	tok := strings.TrimPrefix(strings.Split(auth, ", k=")[0], "vapid t=")
	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(tok, claims); err != nil {
		t.Fatal(err)
	}
	if claims["sub"] != "mailto:admin@umailserver.local" {
		t.Errorf("sub=%v", claims["sub"])
	}
	if claims["aud"] != "https://push.example.com" {
		t.Errorf("aud=%v", claims["aud"])
	}
	exp, _ := claims["exp"].(float64)
	if d := time.Until(time.Unix(int64(exp), 0)); d <= 0 || d > 24*time.Hour {
		t.Errorf("exp out of range: %v", d)
	}
	if fake.reqs[0].Header.Get("Content-Encoding") != "aes128gcm" {
		t.Error("missing aes128gcm")
	}
	for _, subj := range []string{"admin@x.org", "mailto:admin@x.org", "https://x.org/c"} {
		if got := vapidSubscriber(subj); strings.Contains(got, "mailto:") {
			t.Errorf("%s -> %s", subj, got)
		}
	}
}

// F5767: live subscriptions are refreshed on success so cleanup keeps them.
func TestF5767_TouchOnSuccess(t *testing.T) {
	useFake(t, 201)
	svc := newSvc(t)
	p, a := validKeys(t)
	_ = svc.Subscribe("u", &Subscription{ID: "s", Endpoint: "https://a.example.com/1", P256dh: p, Auth: a})
	svc.mu.Lock()
	svc.subscriptions["s"].UpdatedAt = time.Now().Add(-100 * 24 * time.Hour)
	svc.mu.Unlock()
	sub := svc.GetUserSubscriptions("u")[0]
	if err := svc.SendNotification(sub, &Notification{Title: "t"}); err != nil {
		t.Fatal(err)
	}
	_ = svc.CleanExpiredSubscriptions()
	if len(svc.GetUserSubscriptions("u")) != 1 {
		t.Error("actively used subscription was expired")
	}
}

// 404/410 unsubscribes; other errors keep the subscription.
func TestUnsubscribeOnGone(t *testing.T) {
	for status, wantKept := range map[int]bool{404: false, 410: false, 500: true, 429: true} {
		fake := useFake(t, status)
		_ = fake
		svc := newSvc(t)
		p, a := validKeys(t)
		_ = svc.Subscribe("u", &Subscription{ID: "s", Endpoint: "https://a.example.com/1", P256dh: p, Auth: a})
		err := svc.SendNotification(svc.GetUserSubscriptions("u")[0], &Notification{Title: "t"})
		if err == nil {
			t.Errorf("status %d: expected error", status)
		}
		if kept := len(svc.GetUserSubscriptions("u")) == 1; kept != wantKept {
			t.Errorf("status %d: kept=%v", status, kept)
		}
		if _, e := os.Stat(filepath.Join(svc.dataDir, "sub_s.json")); (e == nil) != wantKept {
			t.Errorf("status %d: file presence wrong", status)
		}
	}
}

// F5768: SendToUser fans out concurrently; F5769: empty users are dropped.
func TestF5768_SendToUserConcurrent(t *testing.T) {
	fake := useFake(t, 201)
	svc := newSvc(t)
	for i := 0; i < 5; i++ {
		p, a := validKeys(t)
		_ = svc.Subscribe("u", &Subscription{Endpoint: "https://a.example.com/" + string(rune('a'+i)), P256dh: p, Auth: a})
	}
	if err := svc.SendToUser("u", &Notification{Title: "t"}); err != nil {
		t.Fatal(err)
	}
	if len(fake.reqs) != 5 {
		t.Errorf("reqs=%d", len(fake.reqs))
	}
}

func TestF5769_UserEntryRemovedAndAtomicPersist(t *testing.T) {
	svc := newSvc(t)
	_ = svc.Subscribe("u", &Subscription{ID: "s", Endpoint: "https://a.example.com/1"})
	data, err := os.ReadFile(filepath.Join(svc.dataDir, "sub_s.json"))
	if err != nil || !json.Valid(data) {
		t.Fatalf("persisted file bad: %v", err)
	}
	_ = svc.Unsubscribe("u", "s")
	if svc.GetStats()["totalUsers"].(int) != 0 {
		t.Error("empty user entry retained")
	}
	ents, _ := os.ReadDir(svc.dataDir)
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("temp file left: %s", e.Name())
		}
	}
}

func TestAPIAdapter(t *testing.T) {
	fake := useFake(t, 201)
	svc := newSvc(t)
	p, a := validKeys(t)
	_ = svc.Subscribe("u", &Subscription{Endpoint: "https://a.example.com/1", P256dh: p, Auth: a})
	ad := NewAPIAdapter(svc)
	if err := ad.SendNotification("u", &Notification{Title: "t"}); err != nil || len(fake.reqs) != 1 {
		t.Fatalf("adapter send: %v %d", err, len(fake.reqs))
	}
	if ad.GetVAPIDPublicKey() == "" {
		t.Error("no key")
	}
}
