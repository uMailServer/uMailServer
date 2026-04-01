package imap

import (
	"testing"
	"time"
)

func TestMailboxStruct(t *testing.T) {
	mbox := &Mailbox{
		Name:           "INBOX",
		Exists:         10,
		Recent:         2,
		Unseen:         5,
		UIDValidity:    1234567890,
		UIDNext:        100,
		Flags:          []string{"\\Seen", "\\Flagged"},
		PermanentFlags: []string{"\\Seen", "\\Flagged", "\\Deleted"},
		ReadOnly:       false,
	}

	if mbox.Name != "INBOX" {
		t.Errorf("expected name INBOX, got %s", mbox.Name)
	}
	if mbox.Exists != 10 {
		t.Errorf("expected exists 10, got %d", mbox.Exists)
	}
	if mbox.Recent != 2 {
		t.Errorf("expected recent 2, got %d", mbox.Recent)
	}
}

func TestMessageStruct(t *testing.T) {
	msg := &Message{
		SeqNum:       1,
		UID:          100,
		Flags:        []string{"\\Seen"},
		InternalDate: time.Now(),
		Size:         1024,
		Data:         []byte("test message"),
		Subject:      "Test Subject",
		Date:         "Mon, 01 Jan 2024 00:00:00 +0000",
		From:         "sender@example.com",
		To:           "recipient@example.com",
	}

	if msg.SeqNum != 1 {
		t.Errorf("expected seqNum 1, got %d", msg.SeqNum)
	}
	if msg.UID != 100 {
		t.Errorf("expected uid 100, got %d", msg.UID)
	}
	if msg.Size != 1024 {
		t.Errorf("expected size 1024, got %d", msg.Size)
	}
}

func TestEnvelopeStruct(t *testing.T) {
	env := &Envelope{
		Date:      "Mon, 01 Jan 2024 00:00:00 +0000",
		Subject:   "Test",
		From:      []*Address{{PersonalName: "Test", MailboxName: "test", HostName: "example.com"}},
		To:        []*Address{{MailboxName: "recipient", HostName: "example.com"}},
		MessageID: "<12345@example.com>",
	}

	if env.Subject != "Test" {
		t.Errorf("expected subject Test, got %s", env.Subject)
	}
	if len(env.From) != 1 {
		t.Errorf("expected 1 from address, got %d", len(env.From))
	}
}

func TestAddressStruct(t *testing.T) {
	addr := &Address{
		PersonalName: "John Doe",
		AtDomainList: "",
		MailboxName:  "john",
		HostName:     "example.com",
	}

	if addr.PersonalName != "John Doe" {
		t.Errorf("expected personal name 'John Doe', got %s", addr.PersonalName)
	}
	if addr.MailboxName != "john" {
		t.Errorf("expected mailbox name 'john', got %s", addr.MailboxName)
	}
}

func TestBodyStructureStruct(t *testing.T) {
	bs := &BodyStructure{
		Type:        "text",
		Subtype:     "plain",
		Parameters:  map[string]string{"charset": "utf-8"},
		ID:          "",
		Description: "",
		Encoding:    "7bit",
		Size:        100,
		Lines:       5,
		Parts:       nil,
	}

	if bs.Type != "text" {
		t.Errorf("expected type text, got %s", bs.Type)
	}
	if bs.Subtype != "plain" {
		t.Errorf("expected subtype plain, got %s", bs.Subtype)
	}
	if bs.Size != 100 {
		t.Errorf("expected size 100, got %d", bs.Size)
	}
}

func TestSearchCriteriaStruct(t *testing.T) {
	criteria := &SearchCriteria{
		All:        true,
		Answered:   false,
		Deleted:    false,
		Flagged:    true,
		New:        false,
		Old:        false,
		Recent:     false,
		Seen:       true,
		Unanswered: false,
		Undeleted:  true,
		Unflagged:  false,
		Unseen:     false,
		Draft:      false,
		Undraft:    true,
		SeqSet:     "1:10",
		UIDSet:     "100:110",
		From:       "sender@example.com",
		To:         "recipient@example.com",
		Subject:    "test",
		Body:       "content",
		Text:       "search text",
		Header:     map[string]string{"X-Priority": "1"},
		Larger:     1024,
		Smaller:    10485760,
	}

	if !criteria.All {
		t.Error("expected All to be true")
	}
	if criteria.SeqSet != "1:10" {
		t.Errorf("expected SeqSet '1:10', got %s", criteria.SeqSet)
	}
	if criteria.From != "sender@example.com" {
		t.Errorf("expected From 'sender@example.com', got %s", criteria.From)
	}
}

func TestStatusItemConsts(t *testing.T) {
	if StatusMessages != "MESSAGES" {
		t.Errorf("expected StatusMessages MESSAGES, got %s", StatusMessages)
	}
	if StatusRecent != "RECENT" {
		t.Errorf("expected StatusRecent RECENT, got %s", StatusRecent)
	}
	if StatusUIDNext != "UIDNEXT" {
		t.Errorf("expected StatusUIDNext UIDNEXT, got %s", StatusUIDNext)
	}
	if StatusUIDValidity != "UIDVALIDITY" {
		t.Errorf("expected StatusUIDValidity UIDVALIDITY, got %s", StatusUIDValidity)
	}
	if StatusUnseen != "UNSEEN" {
		t.Errorf("expected StatusUnseen UNSEEN, got %s", StatusUnseen)
	}
}





func TestSearchCriteriaNot(t *testing.T) {
	inner := &SearchCriteria{Seen: true}
	criteria := &SearchCriteria{
		Not: inner,
	}

	if criteria.Not == nil {
		t.Fatal("expected Not to be set")
	}
	if !criteria.Not.Seen {
		t.Error("expected inner criteria Seen to be true")
	}
}

func TestSearchCriteriaOr(t *testing.T) {
	criteria1 := &SearchCriteria{Seen: true}
	criteria2 := &SearchCriteria{Flagged: true}
	criteria := &SearchCriteria{
		Or: [2]*SearchCriteria{criteria1, criteria2},
	}

	if len(criteria.Or) != 2 {
		t.Fatalf("expected Or length 2, got %d", len(criteria.Or))
	}
	if !criteria.Or[0].Seen {
		t.Error("expected first Or criteria Seen to be true")
	}
	if !criteria.Or[1].Flagged {
		t.Error("expected second Or criteria Flagged to be true")
	}
}

func TestSearchCriteriaDateSearches(t *testing.T) {
	now := time.Now()
	criteria := &SearchCriteria{
		Before:     now,
		On:         now,
		Since:      now,
		SentBefore: now,
		SentOn:     now,
		SentSince:  now,
	}

	if criteria.Before.IsZero() {
		t.Error("expected Before to be set")
	}
	if criteria.On.IsZero() {
		t.Error("expected On to be set")
	}
	if criteria.Since.IsZero() {
		t.Error("expected Since to be set")
	}
}

func TestMailboxFields(t *testing.T) {
	mbox := &Mailbox{
		Name:           "Test",
		Exists:         100,
		Recent:         5,
		Unseen:         10,
		UIDValidity:    12345,
		UIDNext:        999,
		Flags:          []string{"\\Seen", "\\Flagged"},
		PermanentFlags: []string{"\\Seen", "\\Deleted"},
		ReadOnly:       true,
	}

	if mbox.UIDValidity != 12345 {
		t.Errorf("expected UIDValidity 12345, got %d", mbox.UIDValidity)
	}
	if mbox.UIDNext != 999 {
		t.Errorf("expected UIDNext 999, got %d", mbox.UIDNext)
	}
	if !mbox.ReadOnly {
		t.Error("expected ReadOnly to be true")
	}
}

func TestMessageFields(t *testing.T) {
	msg := &Message{
		SeqNum:       42,
		UID:          12345,
		Flags:        []string{"\\Seen", "\\Answered"},
		InternalDate: time.Now(),
		Size:         2048,
		Data:         []byte("test message data"),
		Subject:      "Test Subject",
		Date:         "Mon, 01 Jan 2024 00:00:00 +0000",
		From:         "sender@example.com",
		To:           "recipient@example.com",
	}

	if msg.SeqNum != 42 {
		t.Errorf("expected SeqNum 42, got %d", msg.SeqNum)
	}
	if msg.UID != 12345 {
		t.Errorf("expected UID 12345, got %d", msg.UID)
	}
	if msg.Size != 2048 {
		t.Errorf("expected Size 2048, got %d", msg.Size)
	}
}

func TestEnvelopeFields(t *testing.T) {
	env := &Envelope{
		Date:      "Mon, 01 Jan 2024 00:00:00 +0000",
		Subject:   "Test Subject",
		From:      []*Address{{PersonalName: "Sender", MailboxName: "sender", HostName: "example.com"}},
		Sender:    []*Address{{MailboxName: "sender", HostName: "example.com"}},
		ReplyTo:   []*Address{{MailboxName: "reply", HostName: "example.com"}},
		To:        []*Address{{MailboxName: "recipient", HostName: "example.com"}},
		Cc:        []*Address{{MailboxName: "cc", HostName: "example.com"}},
		Bcc:       []*Address{{MailboxName: "bcc", HostName: "example.com"}},
		InReplyTo: "<previous@example.com>",
		MessageID: "<current@example.com>",
	}

	if env.InReplyTo != "<previous@example.com>" {
		t.Errorf("expected InReplyTo '<previous@example.com>', got %s", env.InReplyTo)
	}
	if env.MessageID != "<current@example.com>" {
		t.Errorf("expected MessageID '<current@example.com>', got %s", env.MessageID)
	}
}

func TestAddressFields(t *testing.T) {
	addr := &Address{
		PersonalName: "John Doe",
		AtDomainList: "",
		MailboxName:  "john",
		HostName:     "example.com",
	}

	if addr.PersonalName != "John Doe" {
		t.Errorf("expected PersonalName 'John Doe', got %s", addr.PersonalName)
	}
	if addr.MailboxName != "john" {
		t.Errorf("expected MailboxName 'john', got %s", addr.MailboxName)
	}
	if addr.HostName != "example.com" {
		t.Errorf("expected HostName 'example.com', got %s", addr.HostName)
	}
}

func TestBodyStructureFields(t *testing.T) {
	bs := &BodyStructure{
		Type:        "multipart",
		Subtype:     "mixed",
		Parameters:  map[string]string{"boundary": "abc123"},
		ID:          "<part1>",
		Description: "Test part",
		Encoding:    "base64",
		Size:        1024,
		Lines:       0,
		Parts:       []*BodyStructure{},
	}

	if bs.Type != "multipart" {
		t.Errorf("expected Type 'multipart', got %s", bs.Type)
	}
	if bs.Subtype != "mixed" {
		t.Errorf("expected Subtype 'mixed', got %s", bs.Subtype)
	}
	if bs.ID != "<part1>" {
		t.Errorf("expected ID '<part1>', got %s", bs.ID)
	}
}

func TestSearchCriteriaSizeSearches(t *testing.T) {
	criteria := &SearchCriteria{
		Larger:  1024,
		Smaller: 10485760,
	}

	if criteria.Larger != 1024 {
		t.Errorf("expected Larger 1024, got %d", criteria.Larger)
	}
	if criteria.Smaller != 10485760 {
		t.Errorf("expected Smaller 10485760, got %d", criteria.Smaller)
	}
}

func TestSearchCriteriaHeader(t *testing.T) {
	criteria := &SearchCriteria{
		Header: map[string]string{
			"X-Priority": "1",
			"X-Mailer":   "TestMailer",
		},
	}

	if criteria.Header == nil {
		t.Fatal("expected Header to be set")
	}
	if criteria.Header["X-Priority"] != "1" {
		t.Errorf("expected X-Priority '1', got %s", criteria.Header["X-Priority"])
	}
}
