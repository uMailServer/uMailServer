package config

import (
	"bufio"
	"strings"
	"testing"
)

// F6218: final answer without trailing newline is kept; bad ports/answers fall back.
func TestR139_WizardInput(t *testing.T) {
	w := &SetupWizard{reader: bufio.NewReader(strings.NewReader("/data/x")), Config: DefaultConfig()}
	got, err := w.askString("dir", "def")
	if err != nil || got != "/data/x" {
		t.Fatalf("got %q, %v", got, err)
	}
	w.reader = bufio.NewReader(strings.NewReader("70000\n"))
	if v := w.askInt("p", 25); v != 25 {
		t.Fatalf("out-of-range port accepted: %d", v)
	}
	w.reader = bufio.NewReader(strings.NewReader("yse\n"))
	if !w.askBool("q", true) {
		t.Fatal("unrecognized answer must fall back to default")
	}
}
