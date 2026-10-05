package config

import (
	"math"
	"testing"
)

func TestParseSizeDecimalPrecisionControl(t *testing.T) {
	for _, s := range []string{"9007199254740993", "1.5KB"} {
		v, e := ParseSize(s)
		if e != nil || v <= 0 {
			t.Fatal(v, e)
		}
	}
}
func TestParseSizeDecimalPrecisionFailure(t *testing.T) {
	v, e := ParseSize("9007199254740993.0")
	if e != nil || v != 9007199254740993 {
		t.Fatalf("DEFECT F4755 decimal integer precision: got %d %v want 9007199254740993", v, e)
	}
}
func TestParseSizeDecimalPrecisionEdges(t *testing.T) {
	for _, tc := range []struct {
		s   string
		v   Size
		bad bool
	}{{"9007199254740993.1", 9007199254740993, false}, {"9223372036854775807.0", Size(math.MaxInt64), false}, {"9223372036854775808.0", 0, true}, {"0.5KB", 512, false}, {"1.99B", 1, false}} {
		v, e := ParseSize(tc.s)
		if (e != nil) != tc.bad || (!tc.bad && v != tc.v) {
			t.Fatalf("edge %s got %d %v want %d bad=%v", tc.s, v, e, tc.v, tc.bad)
		}
	}
}
