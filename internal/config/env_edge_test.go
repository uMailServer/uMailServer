package config

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func envTestCfg(t *testing.T) *Config {
	c := DefaultConfig()
	c.Server.DataDir = t.TempDir()
	return c
}

// F6012: env overrides for time.Duration and []string fields were rejected
// ("invalid syntax") or silently ignored.
func TestRegressionF6012EnvDurationAndSlice(t *testing.T) {
	t.Setenv("UMAILSERVER_LDAP_TIMEOUT", "45s")
	t.Setenv("UMAILSERVER_HTTP_CORS_ORIGINS", "https://a.example, https://b.example")
	t.Setenv("UMAILSERVER_SPAM_RBL_SERVERS", "x.example,y.example")
	c := envTestCfg(t)
	if err := loadFromEnv(c); err != nil {
		t.Fatalf("loadFromEnv: %v", err)
	}
	if c.LDAP.Timeout != 45*time.Second {
		t.Errorf("ldap timeout = %v", c.LDAP.Timeout)
	}
	if want := []string{"https://a.example", "https://b.example"}; !reflect.DeepEqual(c.HTTP.CorsOrigins, want) {
		t.Errorf("cors = %#v", c.HTTP.CorsOrigins)
	}
	if want := []string{"x.example", "y.example"}; !reflect.DeepEqual(c.Spam.RBLServers, want) {
		t.Errorf("rbl = %#v", c.Spam.RBLServers)
	}
}

// F6013: an unparsable-type env var must not be silently dropped.
func TestRegressionF6013EnvUnsupportedKindErrors(t *testing.T) {
	var m map[string]string
	if err := setFieldFromString(reflect.ValueOf(&m).Elem(), "a=b"); err == nil {
		t.Error("expected error for unsupported kind")
	}
	// Overflow checks
	var n int32
	if err := setFieldFromString(reflect.ValueOf(&n).Elem(), "99999999999"); err == nil {
		t.Error("expected overflow error")
	}
	_ = filepath.Join
}
