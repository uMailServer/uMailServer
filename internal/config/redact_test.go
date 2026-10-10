package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func secretConfig() *Config {
	c := DefaultConfig()
	c.Security.JWTSecret = "SECRET-JWT-0123456789abcdef0123456789"
	c.Security.TOTPKey = "SECRET-TOTP-0123456789abcdef0123456789"
	c.LDAP.BindPassword = "SECRET-LDAP"
	c.MCP.AuthToken = "SECRET-MCP"
	c.MCP.AdminAuthToken = "SECRET-MCPADMIN"
	c.Alert.SMTPPassword = "SECRET-SMTP"
	c.Alert.WebhookHeaders = map[string]string{"Authorization": "SECRET-HDR"}
	c.Push.VAPIDPrivateKey = "SECRET-VAPID"
	return c
}

// F6011: secrets must not leak via %v/%+v/%#v/%s or JSON of Config.
func TestRegressionF6011SecretsRedacted(t *testing.T) {
	c := secretConfig()
	outs := map[string]string{
		"%v ptr":  fmt.Sprintf("%v", c),
		"%+v ptr": fmt.Sprintf("%+v", c),
		"%+v val": fmt.Sprintf("%+v", *c),
		"%#v":     fmt.Sprintf("%#v", c),
		"%+v sec": fmt.Sprintf("%+v", c.Security),
		"%v ldap": fmt.Sprintf("%v", c.LDAP),
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	outs["json"] = string(b)
	for name, out := range outs {
		if strings.Contains(out, "SECRET") {
			t.Errorf("%s leaks a secret: %s", name, out)
		}
	}
	if !strings.Contains(outs["%+v ptr"], "[REDACTED]") {
		t.Error("expected redaction marker")
	}
	// Persistence must still round-trip the real values.
	y, err := yaml.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(y), "SECRET-JWT") || !strings.Contains(string(y), "SECRET-LDAP") {
		t.Error("yaml marshal must keep real secrets for config save")
	}
	// original untouched
	if c.Security.JWTSecret == "[REDACTED]" {
		t.Error("redaction mutated original")
	}
}
