package server

import (
	"os"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/smtp"
)

// F7310: RET/ENVID/ORCPT given in the SMTP transaction reach the queue entry
// of the matching recipient and the partial-failure DSN.
func TestF7310RelayEntryCarriesDSNParams(t *testing.T) {
	s := newDeliveryAuditServer(t)
	info := &smtp.DSNInfo{Ret: "HDRS", EnvID: "env+2B1", ORCPT: []string{"rfc822;bob-orig@test.com", "rfc822;far-orig@other.org"}}
	err := s.deliverMessageWithDSN("s@ext.com", []string{"bob@test.com", "far@other.org"}, []string{"", ""}, info, []byte(deliveryAuditMsg))
	if err != nil {
		t.Fatal(err)
	}
	e := pendingTo(t, s)["far@other.org"]
	if e == nil {
		t.Fatal("no queue entry")
	}
	if e.EnvID != "env+2B1" || e.ORCPT != "rfc822;far-orig@other.org" || e.Ret != db.DSNRet(1) {
		t.Errorf("entry EnvID=%q ORCPT=%q Ret=%v", e.EnvID, e.ORCPT, e.Ret)
	}
	// Control: the notify-only handler leaves them empty.
	s2 := newDeliveryAuditServer(t)
	if err := s2.deliverMessageWithNotify("s@ext.com", []string{"far@other.org"}, nil, []byte(deliveryAuditMsg)); err != nil {
		t.Fatal(err)
	}
	if e := pendingTo(t, s2)["far@other.org"]; e == nil || e.EnvID != "" || e.ORCPT != "" {
		t.Errorf("control entry = %+v", e)
	}
}

func TestF7310PartialFailureDSNCarriesEnvIDOrcpt(t *testing.T) {
	s := newDeliveryAuditServer(t)
	info := &smtp.DSNInfo{EnvID: "E9", ORCPT: []string{"rfc822;bob-orig@test.com", "rfc822;alice-orig@test.com"}}
	// alice is over quota (fails), bob is delivered: alice's failure is reported.
	if err := s.deliverMessageWithDSN("s@ext.com", []string{"bob@test.com", "alice@test.com"}, nil, info, []byte(deliveryAuditMsg)); err != nil {
		t.Fatal(err)
	}
	e := pendingTo(t, s)["s@ext.com"]
	if e == nil {
		t.Fatal("no failure DSN queued")
	}
	b, err := os.ReadFile(e.MessagePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Original-Envelope-Id: E9\r\n", "Original-Recipient: rfc822; alice-orig@test.com\r\n"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("DSN lacks %q:\n%s", want, b)
		}
	}
	if strings.Contains(string(b), "bob-orig") {
		t.Errorf("DSN names the wrong recipient's ORCPT:\n%s", b)
	}
}
