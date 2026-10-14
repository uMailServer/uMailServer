package metrics

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// HOLDS: Prometheus text is well formed (HELP+TYPE once per family, no
// duplicate series, every sample preceded by its TYPE) and counters stay
// monotonic and exact under concurrent increments (run under -race).
func TestR288PrometheusWellFormedAndMonotonic(t *testing.T) {
	m := &SimpleMetrics{}
	const workers, each = 16, 500
	stop := make(chan struct{})
	var scrapers sync.WaitGroup
	scrapers.Add(1)
	go func() {
		defer scrapers.Done()
		var last uint64
		for {
			select {
			case <-stop:
				return
			default:
			}
			var b bytes.Buffer
			_ = m.WritePrometheus(&b)
			for _, ln := range strings.Split(b.String(), "\n") {
				if strings.HasPrefix(ln, "umailserver_smtp_connections_total ") {
					var v uint64
					if _, err := fmt.Sscanf(ln, "umailserver_smtp_connections_total %d", &v); err == nil {
						if v < last {
							t.Errorf("counter went backwards: %d < %d", v, last)
						}
						last = v
					}
				}
			}
		}
	}()
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < each; j++ {
				m.SMTPConnection()
			}
		}()
	}
	wg.Wait()
	close(stop)
	scrapers.Wait()

	var b bytes.Buffer
	if err := m.WritePrometheus(&b); err != nil {
		t.Fatal(err)
	}
	help, typ, series := map[string]int{}, map[string]int{}, map[string]int{}
	for _, ln := range strings.Split(strings.TrimSpace(b.String()), "\n") {
		f := strings.Fields(ln)
		switch {
		case strings.HasPrefix(ln, "# HELP "):
			help[f[2]]++
		case strings.HasPrefix(ln, "# TYPE "):
			typ[f[2]]++
		default:
			series[f[0]]++
			if typ[f[0]] != 1 {
				t.Errorf("sample %s without preceding TYPE", f[0])
			}
		}
	}
	for name, n := range series {
		if n != 1 || help[name] != 1 || typ[name] != 1 {
			t.Errorf("family %s: series=%d help=%d type=%d", name, n, help[name], typ[name])
		}
	}
	if !strings.Contains(b.String(), "umailserver_smtp_connections_total 8000\n") {
		t.Fatalf("lost increments:\n%s", b.String())
	}
}
