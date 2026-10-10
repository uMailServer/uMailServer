package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F6300/F6301: `config check` fails on a missing explicit file and on unknown
// keys, and passes on a clean file.
func TestCmdConfigCheckF6301(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) (int, string) {
		var out, errb bytes.Buffer
		code := cmdConfig(args, &out, &errb)
		return code, out.String() + errb.String()
	}
	if code, msg := run("check", "--config", filepath.Join(dir, "nope.yaml")); code == 0 || !strings.Contains(msg, "not found") {
		t.Errorf("missing file: %d %q", code, msg)
	}
	good := filepath.Join(dir, "good.yaml")
	if err := os.WriteFile(good, []byte("server:\n  hostname: mx.example.com\n  data_dir: "+filepath.Join(dir, "d")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, msg := run("check", "--config", good); code != 0 {
		t.Errorf("clean config: %d %q", code, msg)
	}
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("server:\n  hostname: mx.example.com\n  data_dir: "+filepath.Join(dir, "d")+"\nspam:\n  thershold: 5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, msg := run("check", "--config", bad); code == 0 || !strings.Contains(msg, "spam.thershold") {
		t.Errorf("typo config: %d %q", code, msg)
	}
	if code, _ := run(); code == 0 {
		t.Error("no subcommand must fail")
	}
}
