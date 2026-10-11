package sieve

import (
	"testing"
	"time"
)

// F6770: re-recording a cached sender must not grow the LRU list, and a
// refreshed sender is the most recent, not the next to be evicted.
func TestR200VacationAccessorBounded(t *testing.T) {
	m := NewManager()
	for i := 0; i < 500; i++ {
		m.CheckAndRecordVacationFor("same@x", 0) // :seconds 0 replies every time
		m.RecordVacationSent("same@x")
	}
	m.CheckAndRecordVacationFor("other@x", time.Hour)
	if len(m.vacationAccessor) != len(m.vacationCache) || len(m.vacationCache) != 2 {
		t.Fatalf("accessor=%d cache=%d, want 2/2", len(m.vacationAccessor), len(m.vacationCache))
	}

	m2 := NewManager()
	m2.vacationMaxSize = 4
	for _, s := range []string{"a", "b", "c", "d"} {
		m2.CheckAndRecordVacationFor(s, 0)
	}
	m2.CheckAndRecordVacationFor("a", 0) // refresh: no eviction, a becomes newest
	if len(m2.vacationCache) != 4 {
		t.Fatalf("refresh evicted entries: %d", len(m2.vacationCache))
	}
	m2.CheckAndRecordVacationFor("e", 0) // overflow evicts the oldest, b
	if _, ok := m2.vacationCache["b"]; ok {
		t.Fatal("oldest sender b should be evicted")
	}
	if _, ok := m2.vacationCache["a"]; !ok {
		t.Fatal("refreshed sender a evicted")
	}
}

// F6772: an unterminated bracketed comment is a syntax error.
func TestR200UnterminatedBracketComment(t *testing.T) {
	for _, ok := range []string{"keep; /* c */\n", "keep; /**/\n", "keep; /* a * b */ keep;"} {
		if _, err := Compile(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"keep; /* x\n", "keep; /*/\n", "/* x", "keep; /* x *"} {
		if _, err := Compile(bad); err == nil {
			t.Errorf("%q compiled", bad)
		}
	}
}
