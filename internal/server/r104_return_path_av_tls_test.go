package server

// Round 104 regression tests: F5860 (Return-Path at final delivery, RFC 5321
// 4.4), F5861 (AV scan error temp-fails instead of accepting unscanned),
// F5862 (no implicit-TLS 465 listener / STARTTLS claim without a certificate).

import (
	"net"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/av"
	"github.com/umailserver/umailserver/internal/config"
	"github.com/umailserver/umailserver/internal/smtp"
)

func TestF5860ReturnPathAddedAtLocalDelivery(t *testing.T) {
	s := newDeliveryAuditServer(t)
	if err := s.deliverLocal("bob", "test.com", "s@ext.com", []byte(deliveryAuditMsg)); err != nil {
		t.Fatal(err)
	}
	m, err := s.storageDB.GetMessageMetadata("bob@test.com", "INBOX", 1)
	if err != nil || m == nil {
		t.Fatalf("metadata: %v", err)
	}
	b, err := s.msgStore.ReadMessage("bob@test.com", m.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "Return-Path: <s@ext.com>\r\n") {
		t.Fatalf("stored message lacks Return-Path: %q", b)
	}
}

func TestF5860AddReturnPath(t *testing.T) {
	got := string(addReturnPath([]byte("Return-Path: <evil@x>\r\n  folded\r\nSubject: a\r\n\r\nReturn-Path: body\r\n"), ""))
	want := "Return-Path: <>\r\nSubject: a\r\n\r\nReturn-Path: body\r\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	got = string(addReturnPath([]byte("Subject: a\n\nb\n"), "a@b\r\nBcc: x"))
	if !strings.HasPrefix(got, "Return-Path: <a@bBcc: x>\r\nSubject: a\n") {
		t.Fatalf("injection or bad prefix: %q", got)
	}
}

type avTestLogger struct{ errs, warns int }

func (l *avTestLogger) Warn(string, ...any)  { l.warns++ }
func (l *avTestLogger) Error(string, ...any) { l.errs++ }

func TestF5861AVScanErrorTempFails(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close() // nothing listens: connect fails
	lg := &avTestLogger{}
	st := &avFailClosedStage{scanner: av.NewScanner(av.Config{Enabled: true, Addr: addr, Action: "reject"}), action: "reject", logger: lg}
	ctx := &smtp.MessageContext{Data: []byte("x"), Headers: map[string][]string{}}
	if r := st.Process(ctx); r != smtp.ResultReject || ctx.RejectionCode != 451 || lg.errs != 1 {
		t.Fatalf("result=%v code=%d errs=%d, want reject 451 logged", r, ctx.RejectionCode, lg.errs)
	}
}

func TestF5862NoCertNoTLSClaims(t *testing.T) {
	cfg := coreBindConfig(t, "submissiontls", 0)
	cfg.TLS = config.TLSConfig{}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	if srv.smtpTLSConfig() != nil {
		t.Fatal("smtpTLSConfig must be nil without a certificate")
	}
	if err := srv.startSubmissionTLSSMTP(); err != nil {
		t.Fatal(err)
	}
	if srv.submissionTLSServer != nil {
		t.Fatal("implicit-TLS listener started without a certificate")
	}
}
