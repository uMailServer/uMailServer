package config

import "testing"

func TestRegressionF5800NegativeSize(t *testing.T) {
	for _, s := range []string{"-5", "-1024"} {
		if v, err := ParseSize(s); err == nil {
			t.Errorf("%q accepted as %d", s, v)
		}
	}
}
func TestRegressionF5801NegMailbox(t *testing.T) {
	c := DefaultConfig()
	c.Server.DataDir = t.TempDir()
	c.Server.Hostname = "h"
	c.Domains = nil
	c.SMTP.Inbound.MaxMessageSize = -1
	if err := c.Validate(); err == nil {
		t.Error("neg msg size ok")
	}
	c.SMTP.Inbound.MaxMessageSize = 1024
	c.Logging.MaxSizeMB = -5
	if err := c.Validate(); err == nil {
		t.Error("neg MaxSizeMB ok")
	}
}
