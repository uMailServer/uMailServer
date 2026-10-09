// Regression tests for F5086: MKCOL with "../" in the address book ID created or overwrote collections in another user's namespace.
// Promoted from .temp_files/case_F5086_carddav_mkcol_traversal_test.go.

package carddav

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func a5086Do(s *Server, user, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.SetBasicAuth(user, "pw")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

func a5086Books(t *testing.T, s *Server, user string) map[string]string {
	t.Helper()
	abs, err := s.storage.GetAddressbooks(user)
	if err != nil {
		t.Fatalf("GetAddressbooks: %v", err)
	}
	out := map[string]string{}
	for _, ab := range abs {
		out[ab.ID] = ab.Name
	}
	return out
}

func TestRegressionF5086Control(t *testing.T) {
	s := NewServer(t.TempDir(), nil)
	w := a5086Do(s, "alice@x.test", "MKCOL", "/dav/addressbooks/mine")
	books := a5086Books(t, s, "alice@x.test")
	_, ok := books["mine"]
	if w.Code != http.StatusCreated || !ok {
		t.Fatal("invalid control")
	}
}

func TestRegressionF5086Failure(t *testing.T) {
	s := NewServer(t.TempDir(), nil)
	if w := a5086Do(s, "bob@x.test", "MKCOL", "/dav/addressbooks/bobab"); w.Code != http.StatusCreated {
		t.Fatalf("fixture %d", w.Code)
	}
	w := a5086Do(s, "alice@x.test", "MKCOL", "/dav/addressbooks/../bob_at_x.test/planted")
	books := a5086Books(t, s, "bob@x.test")
	_, planted := books["../bob_at_x.test/planted"]
	if !planted {
		for id := range books {
			if id != "bobab" {
				planted = true
			}
		}
	}
	if planted || w.Code < 400 {
		t.Fatal("DEFECT F5086: MKCOL addressbook-ID traversal writes into another user's namespace")
	}
}

func TestRegressionF5086Edges(t *testing.T) {
	s := NewServer(t.TempDir(), nil)
	if w := a5086Do(s, "bob@x.test", "MKCOL", "/dav/addressbooks/bobab"); w.Code != http.StatusCreated {
		t.Fatalf("fixture %d", w.Code)
	}
	// Overwriting bob's existing book metadata via traversal must fail.
	a5086Do(s, "alice@x.test", "MKCOL", "/dav/addressbooks/../bob_at_x.test/bobab")
	ab, _ := s.storage.GetAddressbook("bob@x.test", "bobab")
	if ab == nil || ab.Name != "bobab" || ab.ID != "bobab" {
		t.Fatalf("DEFECT F5086: bob's book metadata overwritten: %+v", ab)
	}
	// Nested slash IDs are rejected too.
	if w := a5086Do(s, "alice@x.test", "MKCOL", "/dav/addressbooks/a/b"); w.Code < 400 {
		t.Fatalf("nested MKCOL accepted: %d", w.Code)
	}
}
