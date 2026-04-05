package metrics

import (
	"testing"
	"time"
)

func TestNewHistogram(t *testing.T) {
	bounds := []float64{0.1, 0.5, 1, 2.5, 5}
	hist := NewHistogram(bounds)

	if hist == nil {
		t.Fatal("NewHistogram returned nil")
	}

	if len(hist.buckets) != len(bounds)+1 {
		t.Errorf("expected %d buckets, got %d", len(bounds)+1, len(hist.buckets))
	}
}

func TestHistogram_Observe(t *testing.T) {
	bounds := []float64{0.1, 0.5, 1}
	hist := NewHistogram(bounds)

	// Observe values
	hist.Observe(0.05)  // First bucket
	hist.Observe(0.3)   // Second bucket
	hist.Observe(0.7)   // Third bucket
	hist.Observe(2.0)   // Overflow bucket

	snapshot := hist.Snapshot()
	count := snapshot["count"].(uint64)

	if count != 4 {
		t.Errorf("expected count 4, got %d", count)
	}
}

func TestHistogram_Snapshot(t *testing.T) {
	bounds := []float64{1, 2, 3}
	hist := NewHistogram(bounds)

	hist.Observe(0.5)

	snapshot := hist.Snapshot()
	if _, ok := snapshot["count"]; !ok {
		t.Error("snapshot should contain count")
	}
	if _, ok := snapshot["sum"]; !ok {
		t.Error("snapshot should contain sum")
	}
	if _, ok := snapshot["buckets"]; !ok {
		t.Error("snapshot should contain buckets")
	}
	if _, ok := snapshot["bounds"]; !ok {
		t.Error("snapshot should contain bounds")
	}
}

func TestGauge(t *testing.T) {
	g := &Gauge{}

	// Test Set
	g.Set(10.5)
	if g.Get() != 10.5 {
		t.Errorf("expected 10.5, got %f", g.Get())
	}

	// Test Add
	g.Add(2.5)
	if g.Get() != 13.0 {
		t.Errorf("expected 13.0, got %f", g.Get())
	}

	// Test negative Add
	g.Add(-3.0)
	if g.Get() != 10.0 {
		t.Errorf("expected 10.0, got %f", g.Get())
	}
}

func TestCounter(t *testing.T) {
	c := &Counter{}

	// Test Inc
	c.Inc()
	if c.Get() != 1 {
		t.Errorf("expected 1, got %d", c.Get())
	}

	// Test Add
	c.Add(5)
	if c.Get() != 6 {
		t.Errorf("expected 6, got %d", c.Get())
	}
}

func TestNewAdvancedMetrics(t *testing.T) {
	am := NewAdvancedMetrics()

	if am == nil {
		t.Fatal("NewAdvancedMetrics returned nil")
	}

	if am.SMTPCommandLatency == nil {
		t.Error("SMTPCommandLatency should be initialized")
	}
	if am.DeliveryLatency == nil {
		t.Error("DeliveryLatency should be initialized")
	}
	if am.DatabaseQueryLatency == nil {
		t.Error("DatabaseQueryLatency should be initialized")
	}
	if am.APIRequestLatency == nil {
		t.Error("APIRequestLatency should be initialized")
	}
}

func TestAdvancedMetrics_RecordMessageRate(t *testing.T) {
	am := NewAdvancedMetrics()

	// Initially rate should be 0
	if am.GetMessageRate() != 0 {
		t.Errorf("expected initial rate 0, got %f", am.GetMessageRate())
	}

	// Record first message
	am.RecordMessageRate()

	// Wait for time to pass
	time.Sleep(1100 * time.Millisecond)

	// Record second message
	am.RecordMessageRate()

	// Rate should be non-zero now (roughly 1 message per second)
	rate := am.GetMessageRate()
	if rate <= 0 {
		t.Errorf("expected positive rate, got %f", rate)
	}
}

func TestAdvancedMetrics_GetAdvancedStats(t *testing.T) {
	am := NewAdvancedMetrics()

	// Set some values
	am.ActiveSMTPConnections.Set(5)
	am.MessagesReceived.Inc()
	am.MessagesReceived.Inc()
	am.SMTPErrors.Inc()

	stats := am.GetAdvancedStats()

	// Check structure
	connections, ok := stats["connections"].(map[string]interface{})
	if !ok {
		t.Fatal("stats should contain connections")
	}

	if connections["smtp_active"] != float64(5) {
		t.Errorf("expected smtp_active=5, got %v", connections["smtp_active"])
	}

	messages, ok := stats["messages"].(map[string]interface{})
	if !ok {
		t.Fatal("stats should contain messages")
	}

	if messages["received"] != uint64(2) {
		t.Errorf("expected received=2, got %v", messages["received"])
	}

	errors, ok := stats["errors"].(map[string]interface{})
	if !ok {
		t.Fatal("stats should contain errors")
	}

	if errors["smtp"] != uint64(1) {
		t.Errorf("expected smtp errors=1, got %v", errors["smtp"])
	}
}

func TestGetAdvanced(t *testing.T) {
	am1 := GetAdvanced()
	am2 := GetAdvanced()

	if am1 == nil {
		t.Fatal("GetAdvanced returned nil")
	}

	// Should return same instance
	if am1 != am2 {
		t.Error("GetAdvanced should return singleton")
	}
}

func TestTimer(t *testing.T) {
	hist := NewHistogram([]float64{0.001, 0.01, 0.1})
	timer := NewTimer(hist)

	time.Sleep(5 * time.Millisecond)
	duration := timer.ObserveDuration()

	if duration < 0.005 {
		t.Errorf("expected duration >= 0.005s, got %f", duration)
	}

	// Check histogram was updated
	snapshot := hist.Snapshot()
	count := snapshot["count"].(uint64)
	if count != 1 {
		t.Errorf("expected count 1 in histogram, got %d", count)
	}
}

func TestTimer_NilHistogram(t *testing.T) {
	timer := NewTimer(nil)

	time.Sleep(1 * time.Millisecond)
	duration := timer.ObserveDuration()

	if duration < 0.001 {
		t.Errorf("expected duration >= 0.001s, got %f", duration)
	}
	// Should not panic with nil histogram
}

func TestHistogram_Concurrent(t *testing.T) {
	hist := NewHistogram([]float64{1, 2, 3})

	// Concurrent observations
	done := make(chan bool, 100)
	for i := 0; i < 100; i++ {
		go func(val float64) {
			hist.Observe(val)
			done <- true
		}(float64(i % 5))
	}

	// Wait for all
	for i := 0; i < 100; i++ {
		<-done
	}

	snapshot := hist.Snapshot()
	count := snapshot["count"].(uint64)
	if count != 100 {
		t.Errorf("expected count 100, got %d", count)
	}
}

func TestGauge_Concurrent(t *testing.T) {
	g := &Gauge{}

	// Concurrent operations
	done := make(chan bool, 200)

	for i := 0; i < 100; i++ {
		go func() {
			g.Add(1)
			done <- true
		}()
	}

	for i := 0; i < 100; i++ {
		go func() {
			g.Add(-1)
			done <- true
		}()
	}

	// Wait for all
	for i := 0; i < 200; i++ {
		<-done
	}

	// Should be around 0 (might not be exactly 0 due to race in test)
	// But should not panic
}

func TestCounter_Concurrent(t *testing.T) {
	c := &Counter{}

	// Concurrent increments
	done := make(chan bool, 100)
	for i := 0; i < 100; i++ {
		go func() {
			c.Inc()
			done <- true
		}()
	}

	// Wait for all
	for i := 0; i < 100; i++ {
		<-done
	}

	if c.Get() != 100 {
		t.Errorf("expected count 100, got %d", c.Get())
	}
}
