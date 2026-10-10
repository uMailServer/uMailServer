package config

import (
	"encoding/json"
	"fmt"
)

// redactedMarker replaces non-empty secret values in fmt and JSON output.
// YAML marshalling is deliberately untouched: the setup wizard persists the
// real values with yaml.Marshal.
const redactedMarker = "[REDACTED]"

func redact(s string) string {
	if s == "" {
		return ""
	}
	return redactedMarker
}

func redactMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = redact(v)
	}
	return out
}

// Secret-bearing sections implement fmt.Formatter and json.Marshaler so that
// %v/%+v/%#v/%s and json.Marshal of a Config (or of the section) never expose
// credentials, e.g. in a startup log or an API response.

type (
	plainSecurityConfig SecurityConfig
	plainLDAPConfig     LDAPConfig
	plainMCPConfig      MCPConfig
	plainAlertConfig    AlertConfig
	plainPushConfig     PushConfig
)

func (c SecurityConfig) redacted() plainSecurityConfig {
	c.JWTSecret = redact(c.JWTSecret)
	c.TOTPKey = redact(c.TOTPKey)
	return plainSecurityConfig(c)
}

func (c LDAPConfig) redacted() plainLDAPConfig {
	c.BindPassword = redact(c.BindPassword)
	return plainLDAPConfig(c)
}

func (c MCPConfig) redacted() plainMCPConfig {
	c.AuthToken = redact(c.AuthToken)
	c.AdminAuthToken = redact(c.AdminAuthToken)
	return plainMCPConfig(c)
}

func (c AlertConfig) redacted() plainAlertConfig {
	c.SMTPPassword = redact(c.SMTPPassword)
	c.WebhookHeaders = redactMap(c.WebhookHeaders)
	return plainAlertConfig(c)
}

func (c PushConfig) redacted() plainPushConfig {
	c.VAPIDPrivateKey = redact(c.VAPIDPrivateKey)
	return plainPushConfig(c)
}

func format(f fmt.State, verb rune, v any) {
	fmt.Fprintf(f, fmt.FormatString(f, verb), v)
}

// Format implements fmt.Formatter.
func (c SecurityConfig) Format(f fmt.State, verb rune) { format(f, verb, c.redacted()) }

// Format implements fmt.Formatter.
func (c LDAPConfig) Format(f fmt.State, verb rune) { format(f, verb, c.redacted()) }

// Format implements fmt.Formatter.
func (c MCPConfig) Format(f fmt.State, verb rune) { format(f, verb, c.redacted()) }

// Format implements fmt.Formatter.
func (c AlertConfig) Format(f fmt.State, verb rune) { format(f, verb, c.redacted()) }

// Format implements fmt.Formatter.
func (c PushConfig) Format(f fmt.State, verb rune) { format(f, verb, c.redacted()) }

// MarshalJSON implements json.Marshaler with secrets redacted.
func (c SecurityConfig) MarshalJSON() ([]byte, error) { return json.Marshal(c.redacted()) }

// MarshalJSON implements json.Marshaler with secrets redacted.
func (c LDAPConfig) MarshalJSON() ([]byte, error) { return json.Marshal(c.redacted()) }

// MarshalJSON implements json.Marshaler with secrets redacted.
func (c MCPConfig) MarshalJSON() ([]byte, error) { return json.Marshal(c.redacted()) }

// MarshalJSON implements json.Marshaler with secrets redacted.
func (c AlertConfig) MarshalJSON() ([]byte, error) { return json.Marshal(c.redacted()) }

// MarshalJSON implements json.Marshaler with secrets redacted.
func (c PushConfig) MarshalJSON() ([]byte, error) { return json.Marshal(c.redacted()) }
