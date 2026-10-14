package health

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// F7621 regression: a probe joining an in-flight checker run must not inherit
// the cancellation of the probe that started it. Gated order, no sleeps.
func TestR288JoinedProbeDoesNotInheritStarterCancel(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	mon := NewMonitor("t")
	mon.Register("c", func(ctx context.Context) Check {
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-ctx.Done():
		case <-release:
		}
		if ctx.Err() != nil {
			return Check{Status: StatusUnhealthy, Message: "cancelled"}
		}
		return Check{Status: StatusHealthy, Message: "ok"}
	})
	joined := make(chan struct{}, 1)
	testHookJoin = func() { joined <- struct{}{} }
	defer func() { testHookJoin = nil }()

	ctxA, cancelA := context.WithCancel(context.Background())
	aDone := make(chan Report, 1)
	go func() { aDone <- mon.Check(ctxA) }()
	<-started
	bDone := make(chan Report, 1)
	go func() { bDone <- mon.Check(context.Background()) }()
	<-joined
	cancelA()
	<-aDone
	close(release)
	if got := (<-bDone).Checks[0]; got.Status != StatusHealthy {
		t.Fatalf("joined probe inherited starter's cancellation: %+v", got)
	}
}

// HOLDS: exact threshold boundaries (>= semantics for disk, > for queue).
func TestR288ThresholdBoundaries(t *testing.T) {
	cases := []struct {
		usage float64
		want  Status
	}{{79.99, StatusHealthy}, {80, StatusDegraded}, {89.99, StatusDegraded}, {90, StatusUnhealthy}}
	for _, c := range cases {
		if got, _ := diskStatus(c.usage, 80, 90); got != c.want {
			t.Errorf("disk %.2f: got %s want %s", c.usage, got, c.want)
		}
	}
	q := QueueCheck(fakeStats{QueueStatInfo{Pending: 50}}, 50)(context.Background())
	if q.Status != StatusHealthy {
		t.Errorf("pending == limit must be healthy, got %s", q.Status)
	}
	q = QueueCheck(fakeStats{QueueStatInfo{Pending: 51}}, 50)(context.Background())
	if q.Status != StatusDegraded {
		t.Errorf("pending == limit+1 must be degraded, got %s", q.Status)
	}
}

type fakeStats struct{ s QueueStatInfo }

func (f fakeStats) GetStats() (QueueStatInfo, error) { return f.s, nil }

// HOLDS: TLS expiry uses floor(days): N days + 12h counts as N (warning at
// warningDays=N), N days + 1 day does not; critical at criticalDays boundary.
func TestR288TLSPartialDayRounding(t *testing.T) {
	mk := func(left time.Duration) string {
		key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "x"},
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(left)}
		der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(t.TempDir(), "c.pem")
		if err := os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	day := 24 * time.Hour
	for _, c := range []struct {
		left time.Duration
		want Status
	}{
		{7*day + 12*time.Hour, StatusDegraded},  // floor -> 7 <= warning(7)
		{8*day + time.Hour, StatusHealthy},      // floor -> 8
		{3*day + 12*time.Hour, StatusUnhealthy}, // floor -> 3 <= critical(3)
		{4*day + time.Hour, StatusDegraded},
		{-time.Minute, StatusUnhealthy},
	} {
		got := TLSCertificateCheck(mk(c.left), "", 7, 3)(context.Background())
		if got.Status != c.want {
			t.Errorf("left=%v: got %s (%s) want %s", c.left, got.Status, got.Message, c.want)
		}
	}
}

// HOLDS: a panicking checker yields unhealthy and does not poison later runs.
func TestR288PanickingCheckerBounded(t *testing.T) {
	mon := NewMonitor("t")
	mon.Register("p", func(context.Context) Check { panic("boom") })
	for i := 0; i < 2; i++ {
		r := mon.Check(context.Background())
		if r.Status != StatusUnhealthy || r.Checks[0].Name != "p" {
			t.Fatalf("run %d: %+v", i, r)
		}
	}
}
