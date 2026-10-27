package server

import (
	"encoding/json"
	"strings"
	"testing"
)

// The admin Settings view reflects the running configuration and never
// exposes a secret.
func TestSettingsViewNoSecrets(t *testing.T) {
	srv := junkRTServer(t, false)
	c := srv.config
	secrets := map[string]*string{
		"jwt":   &c.Security.JWTSecret,
		"totp":  &c.Security.TOTPKey,
		"ldap":  &c.LDAP.BindPassword,
		"mcp":   &c.MCP.AuthToken,
		"mcpa":  &c.MCP.AdminAuthToken,
		"alert": &c.Alert.SMTPPassword,
		"vapid": &c.Push.VAPIDPrivateKey,
		"hook":  &c.Alert.WebhookURL,
	}
	for name, p := range secrets {
		*p = "SECRET-" + name + "-0123456789abcdef0123456789"
	}
	c.TLS.MinVersion = "1.3"
	c.SMTP.Inbound.MaxRecipients = 77
	c.Spam.RBLServers = []string{"SECRET-dqs.zen.dq.spamhaus.net", "bl.spamcop.net"}

	view := srv.settings()
	data, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "SECRET-") {
		t.Fatalf("settings view leaks a secret: %s", data)
	}
	if view.Hostname != "mx.test.com" || view.TLS.MinVersion != "1.3" || view.MaxRecipients != 77 ||
		view.Notifications.AlertWebhook != c.Alert.Enabled {
		t.Fatalf("settings view does not reflect the config: %+v", view)
	}
	found := false
	for _, l := range view.Listeners {
		if l.Name == "SMTP (MX)" {
			found = l.Enabled == c.SMTP.Inbound.Enabled && l.Port == c.SMTP.Inbound.Port
		}
	}
	if !found && c.SMTP.Inbound.Port > 0 {
		t.Fatalf("MX listener missing or wrong: %+v", view.Listeners)
	}
}

// Spamhaus DQS zones embed the account key; the Settings page must not show it.
func TestMaskRBLKey(t *testing.T) {
	cases := map[string]string{
		"abcdefghijklmnopqrstuvwxyz.zen.dq.spamhaus.net": "***.zen.dq.spamhaus.net",
		"KEY123.dbl.dq.spamhaus.com.":                    "***.dbl.dq.spamhaus.com.",
		"zen.spamhaus.org":                               "zen.spamhaus.org",
		"bl.spamcop.net":                                 "bl.spamcop.net",
		"dq.spamhaus.net":                                "dq.spamhaus.net",
	}
	for in, want := range cases {
		if got := maskRBLKey(in); got != want {
			t.Errorf("maskRBLKey(%q) = %q, want %q", in, got, want)
		}
	}
}
