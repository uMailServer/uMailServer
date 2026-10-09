package smtp

import (
	"net"
	"testing"

	"github.com/umailserver/umailserver/internal/sieve"
)

func sieveScopeStage(t *testing.T, keys []string, script string) *SieveStage {
	t.Helper()
	m := sieve.NewManager()
	for _, k := range keys {
		if err := m.SetActiveScript(k, "s", script); err != nil {
			t.Fatalf("SetActiveScript(%q): %v", k, err)
		}
	}
	return NewSieveStage(m)
}

func sieveScopeCtx(to ...string) *MessageContext {
	return NewMessageContext(net.ParseIP("192.0.2.1"), "s@x.org", to, []byte("Subject: hi\r\n\r\nbody\r\n"))
}

const sieveScopeReject = "require \"reject\";\nreject \"away\";\n"

// sieveScopeBoth stores the script under the address and the bare login, so
// the F5126/F5127 checks hold whichever key a stage looks up.
var sieveScopeBoth = []string{"alice@a.com", "alice"}

// F5125: a script stored under a bare login ("alice") ran for alice@ any
// domain; scripts are keyed by the full mailbox address.
func TestSieveStage_ScriptNotSharedAcrossDomains(t *testing.T) {
	st := sieveScopeStage(t, []string{"alice"}, sieveScopeReject)
	if r := st.Process(sieveScopeCtx("alice@b.com")); r != ResultAccept {
		t.Fatalf("bare-login script ran for alice@b.com: got %d", r)
	}
	st = sieveScopeStage(t, []string{"alice@a.com"}, sieveScopeReject)
	if r := st.Process(sieveScopeCtx("alice@b.com")); r != ResultAccept {
		t.Fatalf("alice@a.com's script ran for alice@b.com: got %d", r)
	}
	// The owner's own reject is applied by the delivery handler (a DSN),
	// not by the stage.
	if r := st.Process(sieveScopeCtx("alice@a.com")); r != ResultAccept {
		t.Fatalf("owner's reject refused at SMTP time: got %d", r)
	}
}

// F5126: discard (RFC 5228 §4.4) was answered with 550 to the sender.
func TestSieveStage_DiscardIsNotSMTPReject(t *testing.T) {
	st := sieveScopeStage(t, sieveScopeBoth, "discard;\n")
	ctx := sieveScopeCtx("alice@a.com")
	if r := st.Process(ctx); r != ResultAccept || ctx.Rejected {
		t.Fatalf("discard: got %d rejected=%v", r, ctx.Rejected)
	}
}

// F5127: one recipient's reject refused the whole transaction, and the
// stage sent vacation replies that the delivery handler owns.
func TestSieveStage_PerRecipientActionsLeftToDelivery(t *testing.T) {
	st := sieveScopeStage(t, sieveScopeBoth, sieveScopeReject)
	if r := st.Process(sieveScopeCtx("alice@a.com", "bob@a.com")); r != ResultAccept {
		t.Fatalf("multi-recipient transaction refused for one script: got %d", r)
	}
	if r := st.Process(sieveScopeCtx("alice@a.com")); r != ResultAccept {
		t.Fatalf("single-recipient reject refused at SMTP time: got %d", r)
	}

	vst := sieveScopeStage(t, sieveScopeBoth, "require \"vacation\";\nvacation :days 1 \"away\";\n")
	// The old stage started the handler with go after claiming the vacation
	// cache; the claim below is the synchronous evidence of that.
	vst.SetVacationHandler(func(string, string, sieve.VacationAction) {})
	if r := vst.Process(sieveScopeCtx("alice@a.com")); r != ResultAccept {
		t.Fatalf("vacation: got %d", r)
	}
	if !vst.manager.CheckAndRecordVacation(`"alice@a.com":"s@x.org"`, 1) {
		t.Fatal("stage claimed the vacation reply the delivery handler sends")
	}
}
