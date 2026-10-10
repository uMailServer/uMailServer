package push

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"
)

type deadlineRecorder struct{ deadlines chan bool }

func (r *deadlineRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	_, ok := req.Context().Deadline()
	r.deadlines <- ok
	return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}, Request: req}, nil
}

// TestSendNotification_RequestHasDeadline pins F5182: webpush-go used a
// client without a timeout, so a subscriber-chosen endpoint that never
// answers blocked the sending goroutine (and the server's background-task
// slot) forever.
func TestSendNotification_RequestHasDeadline(t *testing.T) {
	rec := &deadlineRecorder{deadlines: make(chan bool, 1)}
	old := pushHTTPClient.Transport
	pushHTTPClient.Transport = rec
	defer func() { pushHTTPClient.Transport = old }()

	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	svc, err := NewService(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	sub := &Subscription{ID: "s", UserID: "u", Endpoint: "https://push.example.invalid/x",
		P256dh: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
		Auth:   base64.RawURLEncoding.EncodeToString(auth)}
	if err := svc.SendNotification(sub, &Notification{Title: "t"}); err != nil {
		t.Fatalf("SendNotification: %v", err)
	}
	if !<-rec.deadlines {
		t.Fatal("push request was sent without a deadline")
	}
}
