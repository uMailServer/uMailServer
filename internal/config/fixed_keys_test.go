package config

// storage.sync, http.enabled and the submission require_auth switches are
// read by no runtime code: message files are always fsynced, the API always
// starts and submission always requires AUTH. Setting one to false used to be
// silently ignored; Load now warns.

import (
	"bytes"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestFixedKeysWarnWhenOverridden(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(old)

	body := "server:\n  hostname: mail.example.com\n  data_dir: " + dd(t) +
		"\nstorage:\n  sync: false\nhttp:\n  enabled: false\n" +
		"smtp:\n  submission:\n    require_auth: false\n  submission_tls:\n    require_auth: true\n"
	if _, err := Load(writeCfg(t, body)); err != nil {
		t.Fatalf("must not fail: %v", err)
	}
	out := buf.String()
	for _, k := range []string{"storage.sync", "http.enabled", "smtp.submission.require_auth"} {
		if !strings.Contains(out, `key="`+k+":") {
			t.Errorf("no warning for %s: %s", k, out)
		}
	}
	if strings.Contains(out, "submission_tls.require_auth") {
		t.Errorf("warned for a key set to its honoured value: %s", out)
	}
}

// Every fixed key names a real setting, and the shipped example only sets
// them to the value the server honours.
func TestFixedKeysExampleAndStruct(t *testing.T) {
	want := map[string]bool{}
	yamlPaths(reflect.TypeOf(Config{}), "", want)
	for _, k := range fixedKeys {
		if p := strings.Join(k.path, "."); !want[p] {
			t.Errorf("fixed key %s is not a Config setting", p)
		}
	}

	data, err := os.ReadFile("../../umailserver.yaml.example")
	if err != nil {
		t.Fatal(err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	if got := findOverriddenFixedKeys(&root); len(got) != 0 {
		t.Fatalf("example overrides fixed keys: %v", got)
	}
}
