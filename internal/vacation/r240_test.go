package vacation

import (
	"testing"
	"time"
)

// F7173: Precedence bulk/list/junk is never answered, even when the config
// leaves ignore_lists / ignore_bulk off.
func TestR240PrecedenceAlwaysSuppressed(t *testing.T) {
	cfg := &Config{Enabled: true, Subject: "s", Message: "m", SendInterval: time.Hour}
	for _, p := range []string{"bulk", "List", " junk "} {
		if _, ok := Eligible(cfg, "u@x.com", "a@b.com", map[string]string{"precedence": p}, time.Now()); ok {
			t.Errorf("Precedence %q must suppress the reply", p)
		}
	}
	if _, ok := Eligible(cfg, "u@x.com", "a@b.com", map[string]string{"Precedence": "first-class"}, time.Now()); !ok {
		t.Error("ordinary precedence must be answered")
	}
}
