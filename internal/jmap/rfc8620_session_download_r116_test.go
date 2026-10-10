package jmap

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/umailserver/umailserver/internal/storage"
)

func r116Server(t *testing.T) (*Server, string, *storage.MessageStore) {
	t.Helper()
	ms, err := storage.NewMessageStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(nil, ms, nil, Config{JWTSecret: "r116-secret"})
	tok, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user@example.com", "exp": time.Now().Add(time.Hour).Unix(),
	}).SignedString([]byte("r116-secret"))
	return srv, tok, ms
}

func r116Do(srv *Server, tok, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

// F5980: RFC 8620 §2 accounts require isPersonal/isReadOnly.
func TestR116SessionAccountFlags(t *testing.T) {
	srv, tok, _ := r116Server(t)
	w := r116Do(srv, tok, "GET", "/jmap/session", "")
	var got struct {
		Accounts map[string]map[string]interface{} `json:"accounts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	acc := got.Accounts["user@example.com"]
	if v, ok := acc["isPersonal"].(bool); !ok || !v {
		t.Errorf("isPersonal = %v, want true", acc["isPersonal"])
	}
	if v, ok := acc["isReadOnly"].(bool); !ok || v {
		t.Errorf("isReadOnly = %v, want false", acc["isReadOnly"])
	}
}

// F5981/F5982: request-level problem types.
func TestR116RequestLevelErrors(t *testing.T) {
	srv, tok, _ := r116Server(t)
	cases := []struct{ name, body, want string }{
		{"notJSON", "{nope", "urn:ietf:params:jmap:error:notJSON"},
		{"notRequest", `{"using":"x"}`, "urn:ietf:params:jmap:error:notRequest"},
		{"unknownCapability", `{"using":["urn:example:bogus"],"methodCalls":[]}`, "urn:ietf:params:jmap:error:unknownCapability"},
	}
	for _, c := range cases {
		w := r116Do(srv, tok, "POST", "/jmap/api", c.body)
		var p map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &p)
		if w.Code != 400 || p["type"] != c.want {
			t.Errorf("%s: status=%d type=%v, want 400 %s", c.name, w.Code, p["type"], c.want)
		}
	}
}

// F5983: download honours ?accept and sets Content-Disposition.
func TestR116DownloadHeaders(t *testing.T) {
	srv, tok, ms := r116Server(t)
	id, err := ms.StoreMessage("user@example.com", []byte("Subject: x\r\n\r\nhi"))
	if err != nil {
		t.Fatal(err)
	}
	w := r116Do(srv, tok, "GET", "/jmap/download/user@example.com/"+id+"/mail.eml?accept=message/rfc822", "")
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "message/rfc822" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, "mail.eml") {
		t.Errorf("Content-Disposition = %q", cd)
	}
}

var _ = http.StatusOK
