package queue

import (
	"testing"
	"time"
)

func TestParseDSNNotify(t *testing.T) {
	tests := []struct {
		input    string
		expected DSNNotify
	}{
		{"NEVER", DSNNotifyNever},
		{"SUCCESS", DSNNotifySuccess},
		{"FAILURE", DSNNotifyFailure},
		{"DELAY", DSNNotifyDelay},
		{"SUCCESS,FAILURE", DSNNotifySuccess | DSNNotifyFailure},
		{"SUCCESS,FAILURE,DELAY", DSNNotifySuccess | DSNNotifyFailure | DSNNotifyDelay},
	}

	for _, tt := range tests {
		result := ParseDSNNotify(tt.input)
		if result != tt.expected {
			t.Errorf("ParseDSNNotify(%q) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestDSNNotify_HasNotify(t *testing.T) {
	notify := DSNNotifySuccess | DSNNotifyFailure

	if !notify.HasNotify(DSNNotifySuccess) {
		t.Error("Expected to have SUCCESS")
	}
	if !notify.HasNotify(DSNNotifyFailure) {
		t.Error("Expected to have FAILURE")
	}
	if notify.HasNotify(DSNNotifyDelay) {
		t.Error("Should not have DELAY")
	}
}

func TestParseDSNRet(t *testing.T) {
	if ParseDSNRet("FULL") != DSNRetFull {
		t.Error("Expected FULL")
	}
	if ParseDSNRet("HDRS") != DSNRetHeaders {
		t.Error("Expected HDRS")
	}
	if ParseDSNRet("unknown") != DSNRetFull {
		t.Error("Expected default FULL")
	}
}

func TestGenerateMessageID(t *testing.T) {
	id1 := GenerateMessageID()
	id2 := GenerateMessageID()

	if id1 == "" {
		t.Error("Expected non-empty message ID")
	}
	if id1 == id2 {
		t.Error("Expected unique message IDs")
	}

	// Should contain @
	if len(id1) < 2 || id1[0] != '<' {
		t.Errorf("Expected message ID format <...>, got %s", id1)
	}
}

func TestGenerateDSN(t *testing.T) {
	dsn := &DSN{
		ReportedDomain: "example.com",
		ReportedName:   "mail.example.com",
		ArrivalDate:    testDate(),
		OriginalFrom:   "sender@example.com",
		OriginalTo:     "recipient@example.com",
		Recipient: DSNRecipient{
			Original: "recipient@example.com",
			Notify:   DSNNotifyFailure,
			Ret:      DSNRetFull,
		},
		Action:         "failed",
		Status:         "5.0.0",
		DiagnosticCode: "550 User unknown",
		RemoteMTA:      "mx.example.com",
		MessageID:      "<test@example.com>",
	}

	msg, err := GenerateDSN(dsn, []byte("Test message content"), DSNRetFull)
	if err != nil {
		t.Fatalf("GenerateDSN error: %v", err)
	}

	if len(msg) == 0 {
		t.Error("Expected non-empty message")
	}
}

func TestGenerateSuccessDSN(t *testing.T) {
	dsn := &DSN{
		ReportedDomain: "example.com",
		ReportedName:   "mail.example.com",
		ArrivalDate:    testDate(),
		OriginalFrom:   "sender@example.com",
		OriginalTo:     "recipient@example.com",
		Recipient: DSNRecipient{
			Original: "recipient@example.com",
		},
		MessageID: "<test@example.com>",
	}

	msg, err := GenerateSuccessDSN(dsn, []byte("Test message"), DSNRetFull)
	if err != nil {
		t.Fatalf("GenerateSuccessDSN error: %v", err)
	}

	if len(msg) == 0 {
		t.Error("Expected non-empty message")
	}
}

func TestGenerateFailureDSN(t *testing.T) {
	dsn := &DSN{
		ReportedDomain: "example.com",
		ReportedName:   "mail.example.com",
		ArrivalDate:    testDate(),
		OriginalFrom:   "sender@example.com",
		OriginalTo:     "recipient@example.com",
		Recipient: DSNRecipient{
			Original: "recipient@example.com",
		},
		MessageID: "<test@example.com>",
	}

	msg, err := GenerateFailureDSN(dsn, []byte("Test message"), DSNRetFull, "550 User unknown")
	if err != nil {
		t.Fatalf("GenerateFailureDSN error: %v", err)
	}

	if len(msg) == 0 {
		t.Error("Expected non-empty message")
	}
}

func TestGenerateDelayDSN(t *testing.T) {
	dsn := &DSN{
		ReportedDomain: "example.com",
		ReportedName:   "mail.example.com",
		ArrivalDate:    testDate(),
		OriginalFrom:   "sender@example.com",
		OriginalTo:     "recipient@example.com",
		Recipient: DSNRecipient{
			Original: "recipient@example.com",
		},
		MessageID: "<test@example.com>",
	}

	msg, err := GenerateDelayDSN(dsn)
	if err != nil {
		t.Fatalf("GenerateDelayDSN error: %v", err)
	}

	if len(msg) == 0 {
		t.Error("Expected non-empty message")
	}
}

func TestExtractHeaders(t *testing.T) {
	msg := "From: test@example.com\r\nTo: recipient@example.com\r\nSubject: Test\r\n\r\nBody content"

	headers := extractHeaders(msg)
	if headers == "" {
		t.Error("Expected headers")
	}
}

func TestParseMDNAddress(t *testing.T) {
	addr, err := ParseMDNAddress("test@example.com")
	if err != nil {
		t.Fatalf("ParseMDNAddress error: %v", err)
	}
	if addr.Original != "test@example.com" {
		t.Errorf("Expected 'test@example.com', got %q", addr.Original)
	}
}

func TestGenerateMDN(t *testing.T) {
	msg, err := GenerateMDN(
		[]byte("Original message content"),
		"sender@example.com",
		"recipient@example.com",
		"<original@example.com>",
		"<ref@example.com>",
		MDNDispositionDisplayed,
		"example.com",
	)
	if err != nil {
		t.Fatalf("GenerateMDN error: %v", err)
	}

	if len(msg) == 0 {
		t.Error("Expected non-empty message")
	}
}

func TestGenerateMDN_DifferentDispositions(t *testing.T) {
	dispositions := []MDNDisposition{
		MDNDispositionDisplayed,
		MDNDispositionDeleted,
		MDNDispositionDispatched,
		MDNDispositionDenied,
		MDNDispositionFailed,
	}

	for _, disp := range dispositions {
		msg, err := GenerateMDN(
			[]byte("Test"),
			"from@example.com",
			"to@example.com",
			"<id@example.com>",
			"<ref@example.com>",
			disp,
			"example.com",
		)
		if err != nil {
			t.Errorf("GenerateMDN with disposition %v: %v", disp, err)
		}
		if len(msg) == 0 {
			t.Errorf("Expected non-empty message for disposition %v", disp)
		}
	}
}

func testDate() time.Time {
	return time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
}
