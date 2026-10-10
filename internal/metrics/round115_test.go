package metrics

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

func TestRound115HistogramUnsortedAndInf(t *testing.T) {
	h := NewHistogram([]float64{10, 1, 5})
	h.Observe(0.5)
	h.Observe(math.Inf(1))
	snap := h.Snapshot()
	b := snap["buckets"].([]uint64)
	if b[0] != 1 || b[3] != 1 {
		t.Errorf("buckets %v", b)
	}
	if _, err := json.Marshal(snap); err != nil {
		t.Errorf("snapshot not encodable: %v", err)
	}
}

func TestRound115MessageRateDecays(t *testing.T) {
	am := NewAdvancedMetrics()
	am.rateMutex.Lock()
	am.messageRate = 5
	am.lastMessageTime = time.Now().Unix() - 100
	am.rateMutex.Unlock()
	if r := am.GetMessageRate(); r > 0.011 {
		t.Errorf("stale rate %v", r)
	}
}
