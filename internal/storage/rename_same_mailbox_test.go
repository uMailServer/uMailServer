package storage

import (
	"fmt"
	"testing"
)

func TestRenameMailboxSameNamePreservesData(t *testing.T) {
	db := setupTestDB(t)
	if err := db.CreateMailbox("alice", "control"); err != nil {
		t.Fatal(err)
	}
	if err := db.StoreMessageMetadata("alice", "control", 7, &MessageMetadata{MessageID: "control-msg", Subject: "meeting"}); err != nil {
		t.Fatal(err)
	}
	if err := db.RenameMailbox("alice", "control", "renamed"); err != nil {
		t.Fatal(err)
	}
	meta, err := db.GetMessageMetadata("alice", "renamed", 7)
	if err != nil || meta == nil || meta.MessageID != "control-msg" {
		t.Fatal("CONTROL FAILED", meta, err)
	}
	fmt.Println("CONTROL EXPECTED: rename retains message | ACTUAL: retained")
	if err := db.CreateMailbox("alice", "archive"); err != nil {
		t.Fatal(err)
	}
	if err := db.StoreMessageMetadata("alice", "archive", 9, &MessageMetadata{MessageID: "archive-msg", Subject: "invoice"}); err != nil {
		t.Fatal(err)
	}
	if err := db.RenameMailbox("alice", "archive", "archive"); err != nil {
		t.Fatal("unexpected self-rename error", err)
	}
	meta, err = db.GetMessageMetadata("alice", "archive", 9)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("EXPECTED: archive-msg preserved | ACTUAL: %v\n", meta)
	if meta == nil || meta.MessageID != "archive-msg" {
		fmt.Println("PROBLEM CONFIRMED")
		t.FailNow()
	}
	fmt.Println("PROBLEM NOT REPRODUCED")
	before, err := db.CurrentChangeState("alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RenameMailbox("alice", "archive", "archive"); err != nil {
		t.Fatal(err)
	}
	after, err := db.CurrentChangeState("alice")
	if err != nil || after != before {
		t.Fatal("self-rename added change entries", before, after, err)
	}
	meta, err = db.GetMessageMetadata("alice", "archive", 9)
	if err != nil || meta.MessageID != "archive-msg" {
		t.Fatal("repeated rename lost data", err)
	}
	if err := db.CreateMailbox("alice", "empty"); err != nil {
		t.Fatal(err)
	}
	if err := db.RenameMailbox("alice", "empty", "empty"); err != nil {
		t.Fatal(err)
	}
	names, err := db.ListMailboxes("alice")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, name := range names {
		if name == "empty" {
			found = true
		}
	}
	if !found {
		t.Fatal("empty mailbox removed")
	}
	fmt.Println("FIX VERIFIED")
}
