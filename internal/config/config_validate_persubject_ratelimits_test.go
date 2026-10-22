package config

import "testing"

// TestValidateRejectsNegativePerSubjectRateLimits: every per-IP/per-user
// rate-limit field shares the limit<=0-disabled semantics in internal/ratelimit
// (each gate is "limit > 0 && ..."), so a negative value — reachable via env
// override or YAML — silently disables an abuse control: the persisted daily
// sending quota, the per-IP connection cap, the inbound message throttles, or
// the recipient cap. 0 is the documented "disabled" value; a negative is
// strictly redundant with it and must be rejected (round-24 Global-pair
// precedent, whole-block adjudication).
func TestValidateRejectsNegativePerSubjectRateLimits(t *testing.T) {
	fields := []struct {
		name  string
		apply func(*Config)
	}{
		{"ip_per_minute", func(c *Config) { c.Security.RateLimit.IPPerMinute = -1 }},
		{"ip_per_hour", func(c *Config) { c.Security.RateLimit.IPPerHour = -1 }},
		{"ip_per_day", func(c *Config) { c.Security.RateLimit.IPPerDay = -1 }},
		{"ip_connections", func(c *Config) { c.Security.RateLimit.IPConnections = -1 }},
		{"user_per_minute", func(c *Config) { c.Security.RateLimit.UserPerMinute = -1 }},
		{"user_per_hour", func(c *Config) { c.Security.RateLimit.UserPerHour = -1 }},
		{"user_per_day", func(c *Config) { c.Security.RateLimit.UserPerDay = -1 }},
		{"user_max_recipients", func(c *Config) { c.Security.RateLimit.UserMaxRecipients = -1 }},
	}
	for _, f := range fields {
		f := f
		t.Run(f.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Server.DataDir = t.TempDir()
			f.apply(cfg)
			if err := cfg.Validate(); err == nil {
				t.Errorf("FAIL: negative security.rate_limit.%s accepted — internal/ratelimit gates on limit > 0, so the control is silently disabled", f.name)
			}
		})
	}
}

// TestValidateAcceptsZeroPerSubjectRateLimits is the control: 0 is the
// documented "disabled" value and must keep validating (adjudicated decision,
// not a blanket positivity rule).
func TestValidateAcceptsZeroPerSubjectRateLimits(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.DataDir = t.TempDir()
	cfg.Security.RateLimit.IPPerMinute = 0
	cfg.Security.RateLimit.IPPerHour = 0
	cfg.Security.RateLimit.IPPerDay = 0
	cfg.Security.RateLimit.IPConnections = 0
	cfg.Security.RateLimit.UserPerMinute = 0
	cfg.Security.RateLimit.UserPerHour = 0
	cfg.Security.RateLimit.UserPerDay = 0
	cfg.Security.RateLimit.UserMaxRecipients = 0
	if err := cfg.Validate(); err != nil {
		t.Fatalf("FAIL: zero (documented disabled) values rejected: %v", err)
	}
}
