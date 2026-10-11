package metrics

import (
	"math"
	"testing"
)

func TestGaugeNonFiniteAndHuge_F6631(t *testing.T) {
	var g Gauge
	g.Set(1.5)
	g.Set(math.NaN())
	g.Set(math.Inf(1))
	if g.Get() != 1.5 {
		t.Fatalf("non-finite Set changed gauge: %v", g.Get())
	}
	g.Set(1e300) // previously int64(1e303): undefined conversion
	if g.Get() != 1e300 {
		t.Fatalf("huge value lost: %v", g.Get())
	}
	g.Add(math.MaxFloat64)
	g.Add(math.MaxFloat64)
	if math.IsInf(g.Get(), 0) || math.IsNaN(g.Get()) {
		t.Fatalf("overflow not clamped: %v", g.Get())
	}
	g.Set(0.0004) // sub-milli precision used to truncate to 0
	if g.Get() != 0.0004 {
		t.Fatalf("precision lost: %v", g.Get())
	}
}

func TestHistogramSumStaysFinite_F6631(t *testing.T) {
	h := NewHistogram([]float64{1})
	h.Observe(math.MaxFloat64)
	h.Observe(math.MaxFloat64)
	if s := h.Snapshot()["sum"].(float64); math.IsInf(s, 0) {
		t.Fatal("sum overflowed to Inf")
	}
}
