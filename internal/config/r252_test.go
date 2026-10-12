package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestR252_QueueMaxAgeDefaultAndBounds(t *testing.T) {
	c := DefaultConfig()
	if c.Queue.MaxAge.ToDuration() != 120*time.Hour {
		t.Fatalf("default = %v", c.Queue.MaxAge.ToDuration())
	}
	for _, tc := range []struct {
		age time.Duration
		ok  bool
	}{
		{0, true}, {-time.Hour, false}, {48 * time.Hour, false}, {MinQueueMaxAge - time.Second, false},
		{MinQueueMaxAge, true}, {120 * time.Hour, true}, {MaxQueueMaxAge, true},
		{MaxQueueMaxAge + time.Second, false},
	} {
		c := r259Base(t)
		c.Queue.MaxAge = Duration(tc.age)
		if err := c.Validate(); (err == nil) != tc.ok {
			t.Errorf("max_age %v: err=%v want ok=%v", tc.age, err, tc.ok)
		}
	}
}

func TestR252_QueueMaxAgeYAML(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.yaml")
	y := "server:\n  hostname: mail.example.com\n  data_dir: " + dir + "\nqueue:\n  max_age: 200h\n"
	if err := os.WriteFile(p, []byte(y), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Queue.MaxAge.ToDuration() != 200*time.Hour {
		t.Fatalf("max_age = %v", c.Queue.MaxAge.ToDuration())
	}
	if err := c.ValidateStrict(); err != nil {
		t.Fatalf("queue.max_age flagged as unknown/invalid: %v", err)
	}
}
