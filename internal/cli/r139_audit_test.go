package cli

import "testing"

// F6217: port-25 failure and RBL listing are critical; inconclusive RBL is not.
func TestR139_CriticalIssues(t *testing.T) {
	crit := []string{
		"SMTP: Port 25 unreachable - remote servers may not be able to deliver mail",
		"SMTP: Port 25 greeting is not 220 (554 no)",
		"RBL [bl.spamcop.net]: code-127.0.0.2",
		"DNS [SPF]: x", "DNS [MX]: x", "DNS [DKIM]: x",
	}
	for _, i := range crit {
		if !isCriticalDeliverabilityIssue(i) {
			t.Errorf("%q should be critical", i)
		}
	}
	for _, i := range []string{"RBL [x]: lookup failed (timeout)", "TLS: issues", "SMTP: Could not read server greeting", "DNS check failed: x"} {
		if isCriticalDeliverabilityIssue(i) {
			t.Errorf("%q should not be critical", i)
		}
	}
}
