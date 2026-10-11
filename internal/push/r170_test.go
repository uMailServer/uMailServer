package push

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestF6504_PerUserSubscriptionCap(t *testing.T) {
	svc, err := NewService(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxSubscriptionsPerUser; i++ {
		if err := svc.Subscribe("u", &Subscription{Endpoint: fmt.Sprintf("https://push.example.com/%d", i)}); err != nil {
			t.Fatalf("sub %d: %v", i, err)
		}
	}
	if err := svc.Subscribe("u", &Subscription{Endpoint: "https://push.example.com/over"}); err == nil {
		t.Fatal("expected cap error")
	}
	// Another user is unaffected.
	if err := svc.Subscribe("v", &Subscription{Endpoint: "https://push.example.com/other"}); err != nil {
		t.Fatal(err)
	}
}

type resubRT struct{ fn func() }

func (r *resubRT) RoundTrip(req *http.Request) (*http.Response, error) {
	r.fn()
	return &http.Response{StatusCode: 410, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}, Request: req}, nil
}

// F6505: a 410 for the old endpoint must not delete a subscription that was
// re-registered under the same ID with a new endpoint in the meantime.
func TestF6505_ExpiryDoesNotDeleteReplacedSubscription(t *testing.T) {
	svc, err := NewService(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := ecdh.P256().GenerateKey(rand.Reader)
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	old := &Subscription{ID: "sub1", Endpoint: "https://push.example.com/old",
		P256dh: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
		Auth:   base64.RawURLEncoding.EncodeToString(auth)}
	if err := svc.Subscribe("u", old); err != nil {
		t.Fatal(err)
	}
	snap := *old

	prev := pushHTTPClient.Transport
	pushHTTPClient.Transport = &resubRT{fn: func() {
		_ = svc.Subscribe("u", &Subscription{ID: "sub1", Endpoint: "https://push.example.com/new"})
	}}
	defer func() { pushHTTPClient.Transport = prev }()

	_ = svc.SendNotification(&snap, &Notification{Title: "t", Body: "b"})
	subs := svc.GetUserSubscriptions("u")
	if len(subs) != 1 || subs[0].Endpoint != "https://push.example.com/new" {
		t.Fatalf("replacement subscription was deleted: %+v", subs)
	}
}
