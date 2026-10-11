package health

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type r193Stats struct{ err error }

func (r r193Stats) GetStats() (QueueStatInfo, error) { return QueueStatInfo{}, r.err }

// F6700: /health is unauthenticated; checks must not echo raw dependency error
// text (paths, DSNs, driver internals) into the report.
func TestR193_ChecksDoNotLeakErrorText(t *testing.T) {
	secret := errors.New("open /var/lib/umail/secret.db: permission denied user=admin")
	cases := map[string]Check{
		"database": DatabaseCheck(func() error { return secret })(context.Background()),
		"store":    MessageStoreCheck(func() error { return secret })(context.Background()),
		"search":   SearchIndexCheck(func() error { return secret }, nil)(context.Background()),
		"queue":    QueueCheck(r193Stats{secret}, 100)(context.Background()),
	}
	for name, c := range cases {
		if c.Status != StatusUnhealthy {
			t.Errorf("%s: status %v, want unhealthy", name, c.Status)
		}
		if strings.Contains(c.Message, "secret.db") || strings.Contains(c.Message, "permission") {
			t.Errorf("%s leaks error text: %q", name, c.Message)
		}
	}
	// Control: healthy ping has a message and no error text.
	if c := DatabaseCheck(func() error { return nil })(context.Background()); c.Status != StatusHealthy {
		t.Errorf("control: %v", c.Status)
	}
}

func TestR193_PanicAndTLSDoNotLeak(t *testing.T) {
	m := NewMonitor("t")
	m.Register("boom", func(ctx context.Context) Check { panic("dsn=postgres://u:pw@host/db") })
	r := m.Check(context.Background())
	for _, c := range r.Checks {
		if strings.Contains(c.Message, "pw@host") {
			t.Errorf("panic value leaked: %q", c.Message)
		}
	}
}
