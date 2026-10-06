package websocket

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// Regression (admin realtime): the SSE handler only accepted an
// X-Auth-Token/Bearer header, but browser EventSource clients can only send
// the HttpOnly "jwt" cookie that authMiddleware already accepts on this
// route — so the admin dashboard could never authenticate (401) even after
// switching transports. The handler must accept the same cookie credential.
func TestSSEServerHandlerAcceptsJWTCookie(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	server := NewSSEServer(logger)
	server.SetAuthFunc(func(token string) (string, bool, error) {
		if token == "cookie-jwt-token" {
			return "cookie-user", true, nil
		}
		return "", false, errors.New("invalid token")
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/sse", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		server.Handler().ServeHTTP(rec, req)
		close(done)
	}()
	time.Sleep(300 * time.Millisecond)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d for cookie-authenticated request, got %d", http.StatusOK, rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %q", got)
	}
	if !strings.Contains(rec.Body.String(), "event: connected") {
		t.Errorf("expected connected event, got body %q", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"user":"cookie-user"`) {
		t.Errorf("expected authenticated user in payload, got %q", rec.Body.String())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return after request context cancellation")
	}
}

// Control: the cookie fallback changes only the credential channel, never
// the validation — an invalid cookie must still be rejected.
func TestSSEServerHandlerRejectsInvalidJWTCookie(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	server := NewSSEServer(logger)
	server.SetAuthFunc(func(token string) (string, bool, error) {
		return "", false, errors.New("invalid token")
	})

	req := httptest.NewRequest(http.MethodGet, "/sse", nil)
	req.AddCookie(&http.Cookie{Name: "jwt", Value: "invalid"})
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}
