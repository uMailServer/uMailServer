package metrics

import (
	"encoding/json"
	"math"
	"testing"
)

func TestR110_HistogramNaN(t *testing.T) {
	b := []float64{1, 2}
	h := NewHistogram(b)
	b[0] = 99
	h.Observe(math.NaN())
	h.Observe(0.5)
	s := h.Snapshot()
	if _, err := json.Marshal(s); err != nil {
		t.Fatalf("snapshot not encodable: %v", err)
	}
	if s["count"].(uint64) != 1 || s["bounds"].([]float64)[0] != 1 {
		t.Fatalf("bad snapshot %v", s)
	}
}
