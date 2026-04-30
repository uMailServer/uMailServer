package ratelimit

import (
	"testing"
	"time"
)

// --- CheckIP retrySecs < 1 paths ---

func TestCheckIP_MinuteRetrySecsUnderOne(t *testing.T) {
	cfg := &Config{IPPerMinute: 10}
	rl := New(nil, cfg)

	rl.ipCounters["1.2.3.4"] = &ipBucket{
		minuteCount: 10,
		minuteReset: time.Now().Add(500 * time.Millisecond),
		hourReset:   time.Now().Add(time.Hour),
		dayReset:    time.Now().Add(24 * time.Hour),
	}

	result := rl.CheckIP("1.2.3.4")
	if result.Allowed {
		t.Error("expected blocked")
	}
	if result.RetryAfter != 1 {
		t.Errorf("expected RetryAfter=1, got %d", result.RetryAfter)
	}
}

func TestCheckIP_HourRetrySecsUnderOne(t *testing.T) {
	cfg := &Config{IPPerMinute: 1000, IPPerHour: 10}
	rl := New(nil, cfg)

	rl.ipCounters["1.2.3.4"] = &ipBucket{
		minuteCount: 0,
		minuteReset: time.Now().Add(time.Minute),
		hourCount:   10,
		hourReset:   time.Now().Add(500 * time.Millisecond),
		dayReset:    time.Now().Add(24 * time.Hour),
	}

	result := rl.CheckIP("1.2.3.4")
	if result.Allowed {
		t.Error("expected blocked")
	}
	if result.RetryAfter != 60 {
		t.Errorf("expected RetryAfter=60, got %d", result.RetryAfter)
	}
}

func TestCheckIP_DayRetrySecsUnderOne(t *testing.T) {
	cfg := &Config{IPPerMinute: 1000, IPPerHour: 1000, IPPerDay: 10}
	rl := New(nil, cfg)

	rl.ipCounters["1.2.3.4"] = &ipBucket{
		minuteCount: 0,
		minuteReset: time.Now().Add(time.Minute),
		hourCount:   0,
		hourReset:   time.Now().Add(time.Hour),
		dayCount:    10,
		dayReset:    time.Now().Add(500 * time.Millisecond),
	}

	result := rl.CheckIP("1.2.3.4")
	if result.Allowed {
		t.Error("expected blocked")
	}
	if result.RetryAfter != 3600 {
		t.Errorf("expected RetryAfter=3600, got %d", result.RetryAfter)
	}
}

// --- CheckUser retrySecs < 1 paths ---

func TestCheckUser_MinuteRetrySecsUnderOne(t *testing.T) {
	cfg := &Config{UserPerMinute: 10}
	rl := New(nil, cfg)

	rl.userCounters["u1"] = &userBucket{
		minuteCount: 10,
		minuteReset: time.Now().Add(500 * time.Millisecond),
		hourReset:   time.Now().Add(time.Hour),
		dayReset:    time.Now().Add(24 * time.Hour),
	}

	result := rl.CheckUser("u1")
	if result.Allowed {
		t.Error("expected blocked")
	}
	if result.RetryAfter != 1 {
		t.Errorf("expected RetryAfter=1, got %d", result.RetryAfter)
	}
}

func TestCheckUser_HourRetrySecsUnderOne(t *testing.T) {
	cfg := &Config{UserPerMinute: 1000, UserPerHour: 10}
	rl := New(nil, cfg)

	rl.userCounters["u1"] = &userBucket{
		minuteCount: 0,
		minuteReset: time.Now().Add(time.Minute),
		hourCount:   10,
		hourReset:   time.Now().Add(500 * time.Millisecond),
		dayReset:    time.Now().Add(24 * time.Hour),
	}

	result := rl.CheckUser("u1")
	if result.Allowed {
		t.Error("expected blocked")
	}
	if result.RetryAfter != 60 {
		t.Errorf("expected RetryAfter=60, got %d", result.RetryAfter)
	}
}

func TestCheckUser_DayRetrySecsUnderOne(t *testing.T) {
	cfg := &Config{UserPerMinute: 1000, UserPerHour: 1000, UserPerDay: 10}
	rl := New(nil, cfg)

	rl.userCounters["u1"] = &userBucket{
		minuteCount: 0,
		minuteReset: time.Now().Add(time.Minute),
		hourCount:   0,
		hourReset:   time.Now().Add(time.Hour),
		dayCount:    10,
		dayReset:    time.Now().Add(500 * time.Millisecond),
		sentToday:   10,
	}

	result := rl.CheckUser("u1")
	if result.Allowed {
		t.Error("expected blocked")
	}
	if result.RetryAfter != 3600 {
		t.Errorf("expected RetryAfter=3600, got %d", result.RetryAfter)
	}
}

// --- CheckGlobal retrySecs < 1 paths ---

func TestCheckGlobal_MinuteRetrySecsUnderOne(t *testing.T) {
	cfg := &Config{GlobalPerMinute: 10}
	rl := New(nil, cfg)

	rl.globalBucket.minuteCount = 10
	rl.globalBucket.minuteReset = time.Now().Add(500 * time.Millisecond)
	rl.globalBucket.hourReset = time.Now().Add(time.Hour)

	result := rl.CheckGlobal()
	if result.Allowed {
		t.Error("expected blocked")
	}
	if result.RetryAfter != 1 {
		t.Errorf("expected RetryAfter=1, got %d", result.RetryAfter)
	}
}

func TestCheckGlobal_HourRetrySecsUnderOne(t *testing.T) {
	cfg := &Config{GlobalPerMinute: 1000, GlobalPerHour: 10}
	rl := New(nil, cfg)

	rl.globalBucket.minuteCount = 0
	rl.globalBucket.minuteReset = time.Now().Add(time.Minute)
	rl.globalBucket.hourCount = 10
	rl.globalBucket.hourReset = time.Now().Add(500 * time.Millisecond)

	result := rl.CheckGlobal()
	if result.Allowed {
		t.Error("expected blocked")
	}
	if result.RetryAfter != 60 {
		t.Errorf("expected RetryAfter=60, got %d", result.RetryAfter)
	}
}

// --- CheckConnection retrySecs < 1 ---

func TestCheckConnection_RetrySecsUnderOne(t *testing.T) {
	cfg := &Config{IPConnections: 2}
	rl := New(nil, cfg)

	rl.connLimits["1.2.3.4"] = &connCounter{
		count: 2,
		until: time.Now().Add(500 * time.Millisecond),
	}

	result := rl.CheckConnection("1.2.3.4")
	if result.Allowed {
		t.Error("expected blocked")
	}
	if result.RetryAfter != 10 {
		t.Errorf("expected RetryAfter=10, got %d", result.RetryAfter)
	}
}

// --- cleanup with count != 0 ---

func TestCleanup_ConnCountNonZero(t *testing.T) {
	cfg := DefaultConfig()
	rl := New(nil, cfg)

	// Add a connection counter with count > 0 but expired until
	rl.connLimits["1.2.3.4"] = &connCounter{
		count: 1,
		until: time.Now().Add(-1 * time.Hour),
	}

	rl.cleanup()

	// Should NOT be deleted because count != 0
	if _, ok := rl.connLimits["1.2.3.4"]; !ok {
		t.Error("expected conn limit to remain because count != 0")
	}
}

func TestCleanup_ConnCountZeroAndExpired(t *testing.T) {
	cfg := DefaultConfig()
	rl := New(nil, cfg)

	// Add a connection counter with count == 0 and expired until
	rl.connLimits["1.2.3.4"] = &connCounter{
		count: 0,
		until: time.Now().Add(-1 * time.Hour),
	}

	rl.cleanup()

	// SHOULD be deleted because count == 0 and expired
	if _, ok := rl.connLimits["1.2.3.4"]; ok {
		t.Error("expected conn limit to be cleaned up")
	}
}
