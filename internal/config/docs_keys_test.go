package config

// docs/configuration.md documented many keys the loader does not know
// (storage.type, queue.retry_schedule, imap.auth_timeout, whole auth: and
// dkim: sections), so operators copying them got "unknown key" warnings and
// none of the behaviour. Every YAML example in it must now load cleanly.

import (
	"os"
	"reflect"
	"regexp"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestConfigDocsUseKnownKeys(t *testing.T) {
	data, err := os.ReadFile("../../docs/configuration.md")
	if err != nil {
		t.Fatal(err)
	}
	blocks := regexp.MustCompile("(?s)```yaml\n(.*?)```").FindAllStringSubmatch(string(data), -1)
	if len(blocks) == 0 {
		t.Fatal("INVALID: no yaml blocks found")
	}
	for i, m := range blocks {
		var root yaml.Node
		if err := yaml.Unmarshal([]byte(m[1]), &root); err != nil {
			t.Errorf("block %d does not parse: %v", i, err)
			continue
		}
		var unknown []string
		findUnknownKeys(&root, reflect.TypeOf(Config{}), "", &unknown)
		if len(unknown) > 0 {
			t.Errorf("block %d uses unknown keys: %v", i, unknown)
		}
		if ign := findIgnoredKeys(&root); len(ign) > 0 {
			t.Errorf("block %d uses ignored keys: %v", i, ign)
		}
		if fixed := findOverriddenFixedKeys(&root); len(fixed) > 0 {
			t.Errorf("block %d overrides fixed keys: %v", i, fixed)
		}
	}
}
