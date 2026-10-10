package spam

import (
	"math"
	"testing"

	"go.etcd.io/bbolt"
)

// F5811: NaN token probabilities must not poison the combined result.
func TestF5811_CombinedProbabilityNaN(t *testing.T) {
	for _, in := range [][]float64{{math.NaN()}, {0.9, math.NaN(), 0.8}, {math.Inf(1), math.Inf(-1)}} {
		got := CombinedProbability(in)
		if math.IsNaN(got) || math.IsInf(got, 0) || got < 0 || got > 1 {
			t.Fatalf("CombinedProbability(%v) = %v", in, got)
		}
	}
}

// F5812: token counters saturate instead of wrapping to zero.
func TestF5812_IncrementTokenSaturates(t *testing.T) {
	db, err := bbolt.Open(t.TempDir()+"/s.db", 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := NewClassifier(db)
	if err := c.Initialize(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := c.IncrementToken(SpamBucket, "tok", math.MaxUint32); err != nil {
			t.Fatal(err)
		}
	}
	_, spam, _ := c.GetTokenFrequency("tok")
	if spam != math.MaxUint32 {
		t.Fatalf("count = %d, want saturated %d", spam, uint32(math.MaxUint32))
	}
}
