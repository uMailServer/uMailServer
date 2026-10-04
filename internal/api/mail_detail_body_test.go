package api

import (
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

func mailConversionFixture(t *testing.T, raw string, flags []string) (*MailHandler, *storage.Database, string) {
	t.Helper()
	database, err := storage.OpenDatabase(t.TempDir() + "/mail.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	store, err := storage.NewMessageStore(t.TempDir() + "/messages")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := database.CreateMailbox("reader@example.com", "INBOX"); err != nil {
		t.Fatal(err)
	}
	id, err := store.StoreMessage("reader@example.com", []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.StoreMessageMetadata("reader@example.com", "INBOX", 1, &storage.MessageMetadata{
		UID: 1, MessageID: id, Flags: flags, Subject: "ordinary subject", InternalDate: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	h := NewMailHandler()
	h.SetStorage(store, database)
	return h, database, id
}

func TestMailDetailBodyControls(t *testing.T) {
	h, _, _ := mailConversionFixture(t, "From: sender@example.com\r\nSubject: ordinary\r\n\r\nordinary body", nil)
	list, err := h.getEmailsFromStorage("reader@example.com", "INBOX")
	if err != nil || len(list) != 1 || list[0].Body != "ordinary body" {
		t.Fatalf("list control failed: %v %v", list, err)
	}
	h, _, id := mailConversionFixture(t, "unstructured body", nil)
	detail, err := h.getEmailFromStorage("reader@example.com", "INBOX", id)
	if err != nil || detail.Body != "unstructured body" {
		t.Fatalf("headerless control failed: %v %v", detail, err)
	}
	t.Log("CONTROL EXPECTED: list body and headerless detail preserved ACTUAL: correct")
}

func TestMailDetailBodyMatchesList(t *testing.T) {
	for _, separator := range []string{"\r\n", "\n"} {
		h, _, id := mailConversionFixture(t, "From: sender@example.com"+separator+"Subject: ordinary"+separator+separator+"ordinary body", nil)
		detail, err := h.getEmailFromStorage("reader@example.com", "INBOX", id)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("EXPECTED: body=%q ACTUAL: body=%q", "ordinary body", detail.Body)
		if detail.Body != "ordinary body" {
			t.Error("DEFECT F4744: detail body includes transport headers")
		}
	}
}

func TestMailDetailBodyBoundaries(t *testing.T) {
	for _, body := range []string{"", "line one\nline two", "line one\r\nline two"} {
		h, _, id := mailConversionFixture(t, "Subject: ordinary\r\n\r\n"+body, nil)
		detail, err := h.getEmailFromStorage("reader@example.com", "INBOX", id)
		if err != nil || detail.Body != body || detail.Preview != body {
			t.Errorf("body %q: detail=%v error=%v", body, detail, err)
		}
	}
}
