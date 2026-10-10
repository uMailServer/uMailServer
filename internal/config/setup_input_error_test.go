package config

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var errSetupInputRegression = errors.New("setup input failed")

type setupInputRegressionReader struct{}

func (setupInputRegressionReader) Read(p []byte) (int, error) { return 0, errSetupInputRegression }
func setupInputRegressionRun(t *testing.T, n int) (string, error) {
	t.Helper()
	dir := t.TempDir()
	lines := make([]string, n)
	lines[0] = dir
	lines[1] = "mail.example.com"
	if n > 12 {
		lines[12] = "admin@example.com" // ACME email (default ACME=yes); F5364 re-asks when blank
	}
	w := NewSetupWizard()
	w.reader = bufio.NewReader(io.MultiReader(strings.NewReader(strings.Join(lines, "\n")+"\n"), setupInputRegressionReader{}))
	_, e := w.Run()
	return filepath.Join(dir, "config.yaml"), e
}
func TestSetupInputRegressionControl(t *testing.T) {
	if _, e := setupInputRegressionRun(t, 18); e != nil {
		t.Fatal(e)
	}
	w := NewSetupWizard()
	w.reader = bufio.NewReader(strings.NewReader(""))
	v, e := w.askChoice("EOF", []string{"a"}, "a")
	if e != nil || v != "a" {
		t.Fatal("EOF compatibility", v, e)
	}
}
func TestSetupInputRegressionFailure(t *testing.T) {
	p, e := setupInputRegressionRun(t, 16)
	_, saved := os.Stat(p)
	if !errors.Is(e, errSetupInputRegression) || saved == nil {
		t.Fatalf("DEFECT F4756 setup swallowed input error: err=%v saved=%v", e, saved == nil)
	}
}
func TestSetupInputRegressionEdges(t *testing.T) {
	for _, n := range []int{12, 17} {
		p, e := setupInputRegressionRun(t, n)
		_, saved := os.Stat(p)
		if !errors.Is(e, errSetupInputRegression) || saved == nil {
			t.Fatalf("input fault at %d err=%v saved=%v", n, e, saved == nil)
		}
	}
}
