package audit

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// F6419: secrets passed in Details must be redacted.
func TestF6419_DetailsRedacted(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.log")
	l, err := NewLogger(p, 1, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = l.Log(Event{Type: LoginFailure, User: "u", Details: map[string]string{
		"password": "hunter2", "Authorization": "Bearer abc", "api_token": "tok", "reason": "bad"}})
	l.Close()
	data, _ := os.ReadFile(p)
	for _, secret := range []string{"hunter2", "Bearer abc", "tok\""} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("secret %q logged: %s", secret, data)
		}
	}
	if !strings.Contains(string(data), "bad") {
		t.Fatal("reason lost")
	}
}

// F6419b: concurrent writers with rotation keep every line intact.
func TestF6419_ConcurrentRotationIntegrity(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.log")
	l, err := NewLogger(p, 1, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				l.LogLoginFailure("u", "1.2.3.4", strings.Repeat("x", 300))
			}
		}()
	}
	wg.Wait()
	l.Close()
	files, _ := filepath.Glob(p + "*")
	total := 0
	for _, f := range files {
		d, _ := os.ReadFile(f)
		for _, ln := range strings.Split(strings.TrimSpace(string(d)), "\n") {
			if ln == "" {
				continue
			}
			if !strings.HasPrefix(ln, "{") || !strings.HasSuffix(ln, "}") {
				t.Fatalf("torn line in %s: %.80s", f, ln)
			}
			total++
		}
	}
	if total != 16000 {
		t.Fatalf("lines = %d, want 16000", total)
	}
}
