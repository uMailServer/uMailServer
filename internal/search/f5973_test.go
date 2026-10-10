package search

import (
	"testing"
	"time"
)

func TestRound115SearchDateAndAttachmentFilters(t *testing.T) {
	svc := NewService(nil, nil, nil)
	idx := NewIndex()
	svc.indexes["u"] = idx
	d := func(s string) time.Time { v, _ := time.Parse("2006-01-02", s); return v }
	idx.Add(&Document{ID: "INBOX:1", Content: "report", Date: d("2024-01-10")})
	idx.Add(&Document{ID: "INBOX:2", Content: "report", Date: d("2024-02-10"), HasAttachment: true})
	idx.Add(&Document{ID: "INBOX:3", Content: "report", Date: d("2024-03-10")})

	ids := func(o MessageSearchOptions) map[uint32]bool {
		o.User, o.Query = "u", "report"
		res, err := svc.Search(o)
		if err != nil {
			t.Fatal(err)
		}
		m := map[uint32]bool{}
		for _, r := range res {
			m[r.UID] = true
		}
		return m
	}
	if got := ids(MessageSearchOptions{DateFrom: "2024-02-01"}); len(got) != 2 || got[1] {
		t.Errorf("DateFrom: %v", got)
	}
	if got := ids(MessageSearchOptions{DateTo: "2024-02-10"}); len(got) != 2 || got[3] {
		t.Errorf("DateTo inclusive: %v", got)
	}
	if got := ids(MessageSearchOptions{HasAttachment: true}); len(got) != 1 || !got[2] {
		t.Errorf("HasAttachment: %v", got)
	}
	// pagination applies after filtering
	res, _ := svc.Search(MessageSearchOptions{User: "u", Query: "report", DateFrom: "2024-02-01", Limit: 1, Offset: 1})
	if len(res) != 1 {
		t.Errorf("paged filtered: %v", res)
	}
	if _, err := svc.Search(MessageSearchOptions{User: "u", Query: "report", DateFrom: "garbage"}); err == nil {
		t.Error("expected error for bad date")
	}
}

func TestRound115SearchLimitCapped(t *testing.T) {
	svc := NewService(nil, nil, nil)
	idx := NewIndex()
	svc.indexes["u"] = idx
	for i := 0; i < maxSearchLimit+50; i++ {
		idx.Add(&Document{ID: "INBOX:" + itoa(i+1), Content: "word"})
	}
	res, _ := svc.Search(MessageSearchOptions{User: "u", Query: "word", Limit: 1 << 30})
	if len(res) != maxSearchLimit {
		t.Errorf("got %d", len(res))
	}
}

func itoa(i int) string {
	return string(rune('0'+i/1000%10)) + string(rune('0'+i/100%10)) + string(rune('0'+i/10%10)) + string(rune('0'+i%10))
}
