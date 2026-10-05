package api

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
	"go.etcd.io/bbolt"
)

func sentMetadataRequest(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/mail/send", bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")
	return r.WithContext(context.WithValue(r.Context(), "user", "reader@example.com"))
}
func TestMailSendMetadataHealthyControl(t *testing.T) {
	h, db, _ := mailConversionFixture(t, "existing inbox body", nil)
	rec := httptest.NewRecorder()
	h.handleMailSend(rec, sentMetadataRequest(`{"to":["recipient@example.com"],"subject":"ordinary","body":"ordinary body"}`))
	if rec.Code != 200 {
		t.Fatalf("healthy send control: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	meta, err := db.GetMessageMetadata("reader@example.com", "Sent", 1)
	if err != nil || response["id"] == "" || meta.MessageID != response["id"] {
		t.Fatalf("healthy metadata control: %v %v", meta, err)
	}
	t.Log("CONTROL EXPECTED: healthy send=200 and Sent metadata persisted ACTUAL: correct")
}
func TestMailSendMetadataFailure(t *testing.T) {
	h, db, _ := mailConversionFixture(t, "existing inbox body", nil)
	if err := db.CreateMailbox("reader@example.com", "Sent"); err != nil {
		t.Fatal(err)
	}
	// Deterministic local dependency failure: the next metadata key is a bucket,
	// so bbolt rejects Put while the mailbox UID allocation remains healthy.
	if err := db.Bolt().Update(func(tx *bbolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte("msgs:reader@example.com:Sent"))
		if err != nil {
			return err
		}
		key := make([]byte, 4)
		binary.BigEndian.PutUint32(key, 1)
		_, err = bucket.CreateBucket(key)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.StoreMessageMetadata("reader@example.com", "Sent", 1, &storage.MessageMetadata{UID: 1}); !errors.Is(err, bbolt.ErrIncompatibleValue) {
		t.Fatalf("failure fixture invalid: %v", err)
	}
	rec := httptest.NewRecorder()
	h.handleMailSend(rec, sentMetadataRequest(`{"to":["recipient@example.com"],"subject":"ordinary","body":"ordinary body"}`))
	t.Logf("EXPECTED: metadata failure status=500 ACTUAL: status=%d body=%s", rec.Code, rec.Body.String())
	if rec.Code != http.StatusInternalServerError {
		t.Error("DEFECT F4749: Sent metadata failure reported as successful send")
	}
}
func TestMailSendMetadataBoundaries(t *testing.T) {
	for _, body := range []string{"", "ordinary second body"} {
		h, db, _ := mailConversionFixture(t, "existing inbox body", nil)
		payload, err := json.Marshal(SendMailRequest{To: []string{"recipient@example.com"}, Subject: "ordinary", Body: body})
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			rec := httptest.NewRecorder()
			h.handleMailSend(rec, sentMetadataRequest(string(payload)))
			if rec.Code != 200 {
				t.Fatalf("healthy repeated send status=%d", rec.Code)
			}
		}
		uids, err := db.GetMessageUIDs("reader@example.com", "Sent")
		if err != nil || len(uids) != 2 {
			t.Fatalf("healthy repeated metadata: %v %v", uids, err)
		}
	}
	h, _, _ := mailConversionFixture(t, "existing inbox body", nil)
	rec := httptest.NewRecorder()
	h.handleMailSend(rec, sentMetadataRequest(`{"to":[]}`))
	if rec.Code != 400 {
		t.Fatalf("empty recipients status=%d", rec.Code)
	}
}
