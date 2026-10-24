package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// captureStdout runs fn with os.Stdout redirected into a pipe and returns
// everything fetchAndPrint wrote, so the operator-visible output can be
// asserted without re-exec.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	fn()

	_ = w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// Regression: fetchAndPrint must deliver the ENTIRE response body to the
// operator. Autoconfig/autodiscover XML routinely exceeds 8 KiB; a single
// 8192-byte Read truncates the tail silently (io.Reader makes no
// full-buffer guarantee) and the ignored read error lets a broken transfer
// print "PASSED!". The tail marker sits beyond the 8 KiB mark, so a
// truncated read can never contain it.
func TestFetchAndPrint_ReadsEntireResponseBody(t *testing.T) {
	const marker = "TAIL-MARKER-7f3d</autodiscover>"
	body := strings.Repeat("x", 9000) + marker + "\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	out := captureStdout(t, func() { fetchAndPrint(srv.URL) })

	if !strings.Contains(out, marker) {
		t.Fatalf("response tail lost: got %d bytes of output, want output containing %q", len(out), marker)
	}
	if !strings.Contains(out, strings.Repeat("x", 9000)) {
		t.Fatalf("response body truncated: output missing the 9000-byte prefix")
	}
}

// Control: a small body (shorter than one 8 KiB read) must round-trip
// intact on both the broken and fixed code paths, proving the harness
// itself is sound.
func TestFetchAndPrint_SmallBodyIntact(t *testing.T) {
	body := "<autodiscover><small>ok</small></autodiscover>\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	out := captureStdout(t, func() { fetchAndPrint(srv.URL) })

	if !strings.Contains(out, "<small>ok</small>") {
		t.Fatalf("small response lost entirely: %q", out)
	}
}
