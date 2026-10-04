package api

import (
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

func TestMailFlagCanonicalControls(t *testing.T) {
	h, _, id := mailConversionFixture(t, "ordinary body", []string{"\\Seen", "\\Flagged"})
	detail, err := h.getEmailFromStorage("reader@example.com", "INBOX", id)
	if err != nil || !detail.Read || !detail.Starred {
		t.Fatalf("canonical flag control failed: %v %v", detail, err)
	}
	if hasFlag(nil, "\\Seen") || hasFlag([]string{"\\Seen-extra"}, "\\Seen") {
		t.Fatal("absent flag control failed")
	}
	t.Log("CONTROL EXPECTED: canonical flags true, absent flags false ACTUAL: correct")
}

func TestMailMixedCaseFlagConversion(t *testing.T) {
	flags := []string{"\\sEeN", "\\fLaGgEd"}
	if !storage.HasFlag(flags, "\\Seen") || !storage.HasFlag(flags, "\\Flagged") {
		t.Fatal("storage flag-equivalence control failed")
	}
	h, database, id := mailConversionFixture(t, "ordinary body", flags)
	detail, err := h.getEmailFromStorage("reader@example.com", "INBOX", id)
	if err != nil {
		t.Fatal(err)
	}
	list, err := h.getEmailsFromStorage("reader@example.com", "INBOX")
	if err != nil || len(list) != 1 {
		t.Fatalf("list fixture failed: %v %v", list, err)
	}
	t.Logf("EXPECTED: read=true starred=true ACTUAL: detail=%v/%v list=%v/%v", detail.Read, detail.Starred, list[0].Read, list[0].Starred)
	if !detail.Read || !detail.Starred || !list[0].Read || !list[0].Starred {
		t.Error("DEFECT F4745: equivalent mixed-case flags ignored")
	}
	h.markAsRead("reader@example.com", "INBOX", id)
	meta, err := database.GetMessageMetadata("reader@example.com", "INBOX", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Flags) != 2 {
		t.Errorf("DEFECT F4745: duplicate Seen flag appended: %v", meta.Flags)
	}
}

func TestMailFlagRepeatedMarkRead(t *testing.T) {
	for _, flags := range [][]string{{"\\SEEN"}, {"\\seen"}, nil} {
		h, database, id := mailConversionFixture(t, "ordinary body", flags)
		for i := 0; i < 2; i++ {
			h.markAsRead("reader@example.com", "INBOX", id)
		}
		meta, err := database.GetMessageMetadata("reader@example.com", "INBOX", 1)
		if err != nil || len(meta.Flags) != 1 || !hasFlag(meta.Flags, "\\Seen") {
			t.Errorf("repeated mark: %v %v", meta, err)
		}
	}
}
