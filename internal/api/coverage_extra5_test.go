package api

import (
	"testing"
)

// --- Server setter tests ---

func TestSetRateLimitManager(t *testing.T) {
	s := &Server{}
	mgr := &mockRateLimitManager{}

	s.SetRateLimitManager(mgr)

	if s.rateLimitMgr != mgr {
		t.Error("Rate limit manager not set correctly")
	}
}
