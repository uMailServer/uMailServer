package config

import "testing"

// Every other rate-limit field rejects negative values ("must be
// non-negative", mirroring ratelimit's limit<=0-means-disabled semantics);
// the Global pair only got upper bounds ("effectively unlimited"), so a
// negative value — reachable via env override or YAML — passed validation
// and silently disabled the global rate-limit backstop
// (internal/ratelimit.globalLimit: limit > 0 gates the whole check).
func TestValidateRejectsNegativeGlobalRateLimits(t *testing.T) {
	mkValid := func() *Config {
		cfg := DefaultConfig()
		cfg.Server.DataDir = t.TempDir()
		return cfg
	}

	t.Run("global_per_minute", func(t *testing.T) {
		cfg := mkValid()
		cfg.Security.RateLimit.GlobalPerMinute = -1
		if err := cfg.Validate(); err == nil {
			t.Fatalf("FAIL: negative security.rate_limit.global_per_minute accepted — ratelimit.globalLimit treats limit <= 0 as disabled, so the global backstop is silently off")
		}
	})

	t.Run("global_per_hour", func(t *testing.T) {
		cfg := mkValid()
		cfg.Security.RateLimit.GlobalPerHour = -1
		if err := cfg.Validate(); err == nil {
			t.Fatalf("FAIL: negative security.rate_limit.global_per_hour accepted — same silent-disable path")
		}
	})

	t.Run("valid config control", func(t *testing.T) {
		if err := mkValid().Validate(); err != nil {
			t.Fatalf("FAIL: valid config must keep validating: %v", err)
		}
	})
}
