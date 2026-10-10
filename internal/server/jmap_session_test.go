package server

import (
	"testing"
	"time"
)

// F6250-F6252: JMAP tokens obey the same session cut-off as the HTTP API.
func TestJMAPClaimsValidatorSessionCutoff(t *testing.T) {
	s := newDeliveryAuditServer(t)

	acct, err := s.database.GetAccount("test.com", "bob")
	if err != nil || acct == nil {
		t.Fatalf("get bob: %v", err)
	}

	time.Sleep(5 * time.Millisecond) // issue after the account's creation time
	issued := time.Now()
	claims := map[string]interface{}{
		"sub":  "bob@test.com",
		"iat":  float64(issued.Unix()),
		"iatu": float64(issued.UnixMicro()),
	}
	if err := s.jmapClaimsValidator(claims); err != nil {
		t.Fatalf("fresh session rejected: %v", err)
	}

	// Unknown account (deleted): rejected.
	if err := s.jmapClaimsValidator(map[string]interface{}{"sub": "ghost@test.com"}); err == nil {
		t.Error("token for a missing account was accepted")
	}

	// A cut-off after the token's issue time revokes it.
	time.Sleep(5 * time.Millisecond)
	acct.TokensValidAfter = time.Now().UnixNano()
	if err := s.database.UpdateAccount(acct); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := s.jmapClaimsValidator(claims); err == nil {
		t.Error("token issued before the cut-off was accepted")
	}

	// A token issued after the cut-off stays valid.
	time.Sleep(5 * time.Millisecond)
	later := time.Now()
	claims["iat"] = float64(later.Unix())
	claims["iatu"] = float64(later.UnixMicro())
	if err := s.jmapClaimsValidator(claims); err != nil {
		t.Errorf("token issued after the cut-off rejected: %v", err)
	}
}
