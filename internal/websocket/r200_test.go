package websocket

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// F6771: the Authorization scheme is matched case-insensitively.
func TestR200SSEBearerSchemeCaseInsensitive(t *testing.T) {
	s := NewSSEServer(slog.Default())
	s.SetAuthFunc(func(tok string) (string, bool, error) {
		if tok == "good" {
			return "u@x", false, nil
		}
		return "", false, fmt.Errorf("bad")
	})
	status := func(auth string) int {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		req := httptest.NewRequest("GET", "/events", nil).WithContext(ctx)
		req.Header.Set("Authorization", auth)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec.Code
	}
	for _, a := range []string{"Bearer good", "bearer good", "BEARER good"} {
		if got := status(a); got != http.StatusOK {
			t.Errorf("%q -> %d, want 200", a, got)
		}
	}
	for _, a := range []string{"Bearer bad", "Bearer ", "Bearer", "Basic good", "good"} {
		if got := status(a); got != http.StatusUnauthorized {
			t.Errorf("%q -> %d, want 401", a, got)
		}
	}
}
