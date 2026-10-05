package search

import "testing"

func fieldQueryIndex() *Index {
	idx := NewIndex()
	idx.Add(&Document{ID: "target", Fields: map[string]string{"subject": "Release-v2", "from": "alice@example.com"}})
	idx.Add(&Document{ID: "body-only", Content: "release v2 alice example com", Fields: map[string]string{"subject": "unrelated", "from": "unrelated"}})
	return idx
}
func TestFieldQueryControls(t *testing.T) {
	idx := fieldQueryIndex()
	results := idx.Search("subject:release", SearchOptions{})
	if len(results) != 1 || results[0].DocID != "target" {
		t.Fatalf("simple field control: %v", results)
	}
	if results = idx.Search("release-v2", SearchOptions{}); len(results) != 2 {
		t.Fatalf("general punctuation control: %v", results)
	}
	t.Log("CONTROL EXPECTED: simple field matches target; general punctuation matches both ACTUAL: correct")
}
func TestFieldQueryTokenNormalization(t *testing.T) {
	idx := fieldQueryIndex()
	for _, query := range []string{"subject:release-v2", "subject:release!", "from:alice@example.com"} {
		results := idx.Search(query, SearchOptions{})
		t.Logf("EXPECTED: query=%q field-only target ACTUAL: results=%v", query, results)
		if len(results) != 1 || results[0].DocID != "target" {
			t.Errorf("DEFECT F4751: field query does not use indexed token normalization: %q", query)
		}
	}
}
func TestFieldQueryTokenBoundaries(t *testing.T) {
	idx := fieldQueryIndex()
	for _, query := range []string{"subject:---", "subject:the", ""} {
		if results := idx.Search(query, SearchOptions{}); len(results) != 0 {
			t.Errorf("empty/stopword query %q: %v", query, results)
		}
	}
	idx.Add(&Document{ID: "unicode", Fields: map[string]string{"subject": "ğüneş—ışık"}})
	if results := idx.Search("SUBJECT:ĞÜNEŞ—ışık", SearchOptions{}); len(results) != 1 || results[0].DocID != "unicode" {
		t.Errorf("unicode/case query: %v", results)
	}
	idx.Remove("target")
	if results := idx.Search("subject:release-v2", SearchOptions{}); len(results) != 0 {
		t.Errorf("removed field document remains: %v", results)
	}
}
