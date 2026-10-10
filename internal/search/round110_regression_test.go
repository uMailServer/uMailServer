package search

import "testing"

func TestR110_FieldCaseAndGrowth(t *testing.T) {
	idx := NewIndex()
	idx.Add(&Document{ID: "1", Fields: map[string]string{"From": "alice@example.com"}})
	if r := idx.Search("from:alice", SearchOptions{}); len(r) != 1 {
		t.Fatalf("field name case: %v", r)
	}
	for i := 0; i < 100; i++ {
		idx.Add(&Document{ID: "x", Content: "uniqueword" + string(rune('a'+i%26)) + string(rune('a'+i/26))})
	}
	idx.Remove("x")
	if len(idx.tokens) != 6 {
		t.Fatalf("empty token maps leaked: %d", len(idx.tokens))
	}
}

func TestR110_Unicode(t *testing.T) {
	idx := NewIndex()
	idx.Add(&Document{ID: "1", Content: "ISPARTA ışık İstanbul"})
	idx.Add(&Document{ID: "2", Content: "café menu"}) // NFD
	for _, q := range []string{"ısparta", "isparta", "istanbul", "İSTANBUL"} {
		if r := idx.Search(q, SearchOptions{}); len(r) != 1 || r[0].DocID != "1" {
			t.Errorf("query %q: %v", q, r)
		}
	}
	if r := idx.Search("cafe\u0301", SearchOptions{}); len(r) != 1 || r[0].DocID != "2" {
		t.Errorf("NFD café: %v tok=%v", r, tokenize("café"))
	}
}

func TestR110_IndexMessageNilDB(t *testing.T) {
	s := NewService(nil, nil, nil)
	s.indexes["u"] = NewIndex()
	if err := s.IndexMessage("u", "INBOX", 1); err == nil {
		t.Fatal("expected error, got nil")
	}
}
