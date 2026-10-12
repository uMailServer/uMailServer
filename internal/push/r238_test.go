package push

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"testing"
)

type failRT struct{}

func (failRT) RoundTrip(*http.Request) (*http.Response, error) { return nil, errors.New("boom") }

// F7151: the endpoint (a bearer capability) must not appear in send errors.
func TestF7151_SendErrorOmitsEndpoint(t *testing.T) {
	old := pushHTTPClient
	pushHTTPClient = &http.Client{Transport: failRT{}}
	defer func() { pushHTTPClient = old }()
	s, err := NewService(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ep := "https://push.example.com/send/SECRETTOKEN123"
	err = s.SendNotification(&Subscription{ID: "a", Endpoint: ep, P256dh: "BNcRdreALRFXTkOOUHK1EtK2wtaz5Ry4YfYCA_0QTpQtUbVlUls0VJXg7A8u-Ts1XbjhazAkj7I99e8QcYP7DkM", Auth: "tBHItJI5svbpez7KI4CCXg"}, &Notification{Title: "t"})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "SECRETTOKEN123") {
		t.Fatalf("endpoint leaked: %v", err)
	}
}

func TestF7152_PushForbidsNAT64(t *testing.T) {
	for _, a := range []string{"64:ff9b::7f00:1", "2002:7f00:1::1", "fec0::1"} {
		if !isForbiddenAddr(netip.MustParseAddr(a)) {
			t.Errorf("%s must be forbidden", a)
		}
	}
	if err := validateEndpoint("https://[64:ff9b::a00:1]/x"); err == nil {
		t.Error("endpoint must be rejected")
	}
}
