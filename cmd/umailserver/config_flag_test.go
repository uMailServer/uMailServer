package main

// F7781: domain/account/queue/... took no --config flag: `domain add x
// --config f` swallowed it and wrote to the default database.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestExtractConfigFlag(t *testing.T) {
	for _, c := range []struct {
		in       []string
		wantRest []string
		wantPath string
		wantErr  bool
	}{
		{[]string{"add", "d.example"}, []string{"add", "d.example"}, "", false},
		{[]string{"add", "d.example", "--config", "c.yaml"}, []string{"add", "d.example"}, "c.yaml", false},
		{[]string{"--config", "c.yaml", "add", "d.example"}, []string{"add", "d.example"}, "c.yaml", false},
		{[]string{"-config", "c.yaml", "list"}, []string{"list"}, "c.yaml", false},
		{[]string{"list", "--config=c.yaml"}, []string{"list"}, "c.yaml", false},
		{[]string{"add", "a@b.c", "--password", "x"}, []string{"add", "a@b.c", "--password", "x"}, "", false},
		{[]string{"add", "--configure"}, []string{"add", "--configure"}, "", false}, // not the flag
		{[]string{"add", "---config", "x"}, []string{"add", "---config", "x"}, "", false},
		{[]string{"list", "--config"}, nil, "", true},
		{[]string{"list", "--config="}, nil, "", true},
	} {
		rest, path, err := extractConfigFlag(c.in)
		if (err != nil) != c.wantErr || path != c.wantPath || (!c.wantErr && !reflect.DeepEqual(rest, c.wantRest)) {
			t.Errorf("%v: rest=%v path=%q err=%v; want rest=%v path=%q err=%v", c.in, rest, path, err, c.wantRest, c.wantPath, c.wantErr)
		}
	}
}

func TestApplyConfigFlag(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "c.yaml")
	body := "server:\n  hostname: mail.test\n  data_dir: " + dir + "\nsecurity:\n  jwt_secret: 0123456789abcdef0123456789abcdef0123\n"
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	t.Setenv("UMAILSERVER_CONFIG", "")

	// No flag: nothing changes.
	args := []string{"umailserver", "domain", "list"}
	if err := applyConfigFlag(args); err != nil || os.Getenv("UMAILSERVER_CONFIG") != "" {
		t.Fatalf("no flag: err=%v env=%q", err, os.Getenv("UMAILSERVER_CONFIG"))
	}

	// With the flag: the file becomes the discovered config and is removed
	// from the arguments the subcommand sees.
	args = []string{"umailserver", "domain", "add", "d.example", "--config", cfg}
	if err := applyConfigFlag(args); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("UMAILSERVER_CONFIG"); got != cfg {
		t.Errorf("UMAILSERVER_CONFIG = %q", got)
	}
	if !reflect.DeepEqual(os.Args, []string{"umailserver", "domain", "add", "d.example"}) {
		t.Errorf("os.Args = %v", os.Args)
	}
	if c := discoverConfig(); c == nil || c.Server.DataDir != dir {
		t.Errorf("discoverConfig() = %+v, want the file's data_dir %s", c, dir)
	}

	// A file that is missing or does not load is an error, never a silent
	// fallback to the default database.
	for _, bad := range []string{filepath.Join(dir, "missing.yaml"), func() string {
		p := filepath.Join(dir, "bad.yaml")
		_ = os.WriteFile(p, []byte("server: [not a map"), 0o600)
		return p
	}()} {
		err := applyConfigFlag([]string{"umailserver", "domain", "list", "--config", bad})
		if err == nil || !strings.Contains(err.Error(), bad) {
			t.Errorf("%s: err = %v", bad, err)
		}
	}
}

func TestUsesDiscoveredConfig(t *testing.T) {
	for cmd, want := range map[string]bool{
		"domain": true, "account": true, "queue": true, "check": true, "status": true,
		// These define a --config flag of their own.
		"serve": false, "quickstart": false, "backup": false, "restore": false, "migrate": false, "config": false,
	} {
		if got := usesDiscoveredConfig(cmd); got != want {
			t.Errorf("usesDiscoveredConfig(%q) = %v", cmd, got)
		}
	}
}
