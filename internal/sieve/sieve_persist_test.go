package sieve

// Regression tests for F5630 (Sieve scripts persisted across restarts) and
// F5632 (a refused pre-login PUTSCRIPT/CHECKSCRIPT must not leave its literal
// on the wire). See .temp_files/ledger_internal_sieve_manager_go.md, Round 81.

import (
	"bufio"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

func persistDB(t *testing.T) (*bbolt.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "p.db")
	db, err := bbolt.Open(path, 0o600, &bbolt.Options{Timeout: time.Second})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, path
}

func persistMgr(t *testing.T, db *bbolt.DB, warns *[]string) *Manager {
	t.Helper()
	m := NewManager()
	var w func(string, ...any)
	if warns != nil {
		w = func(msg string, _ ...any) { *warns = append(*warns, msg) }
	}
	if err := m.AttachStore(db, w); err != nil {
		t.Fatalf("AttachStore: %v", err)
	}
	return m
}

func TestSievePersistRoundTrip(t *testing.T) {
	db, _ := persistDB(t)
	m := persistMgr(t, db, nil)
	if err := m.StoreScript("a@x.test", "main", "keep;"); err != nil {
		t.Fatal(err)
	}
	if err := m.StoreScript("a@x.test", "spare", "discard;"); err != nil {
		t.Fatal(err)
	}
	if err := m.SetActiveScriptByName("a@x.test", "main"); err != nil {
		t.Fatal(err)
	}
	if err := m.StoreScript("b@x.test", "vac", "require \"vacation\";\nvacation \"away\";"); err != nil {
		t.Fatal(err)
	}

	m2 := persistMgr(t, db, nil) // restart
	if got := m2.GetActiveScriptName("a@x.test"); got != "main" {
		t.Fatalf("active = %q, want main", got)
	}
	if src, ok := m2.scriptSource("a@x.test", "spare"); !ok || src != "discard;" {
		t.Fatalf("spare = %q %v", src, ok)
	}
	if _, ok := m2.GetActiveScript("a@x.test"); !ok {
		t.Fatal("active script not compiled after load")
	}
	if m2.HasActiveScript("b@x.test") || len(m2.ListScripts("b@x.test")) != 1 {
		t.Fatalf("b: active=%v scripts=%v", m2.HasActiveScript("b@x.test"), m2.ListScripts("b@x.test"))
	}
	// Control: memory-only manager keeps nothing.
	if NewManager().HasActiveScript("a@x.test") {
		t.Fatal("control")
	}
}

func TestSievePersistMutations(t *testing.T) {
	db, _ := persistDB(t)
	m := persistMgr(t, db, nil)
	u := "a@x.test"
	_ = m.StoreScript(u, "one", "keep;")
	_ = m.StoreScript(u, "two", "keep;")
	_ = m.SetActiveScriptByName(u, "one")
	if err := m.renameScript(u, "one", "uno"); err != nil {
		t.Fatal(err)
	}
	m2 := persistMgr(t, db, nil)
	if m2.GetActiveScriptName(u) != "uno" {
		t.Fatalf("rename lost: active %q", m2.GetActiveScriptName(u))
	}
	if err := m.deactivateScript(u); err != nil {
		t.Fatal(err)
	}
	if err := m.deleteInactiveScript(u, "two"); err != nil {
		t.Fatal(err)
	}
	m3 := persistMgr(t, db, nil)
	if m3.HasActiveScript(u) || len(m3.ListScripts(u)) != 1 {
		t.Fatalf("deactivate/delete lost: active=%q scripts=%v", m3.GetActiveScriptName(u), m3.ListScripts(u))
	}
	// Removing the last script removes the record.
	_ = m.deleteInactiveScript(u, "uno")
	var n int
	_ = db.View(func(tx *bbolt.Tx) error { n = tx.Bucket([]byte(scriptsBucket)).Stats().KeyN; return nil })
	if n != 0 {
		t.Fatalf("%d records left", n)
	}
	// Failed operations are not persisted either (control).
	if err := m.deleteInactiveScript(u, "none"); err != errScriptNotFound {
		t.Fatalf("err = %v", err)
	}
}

func TestSievePersistCorruptRecordsTolerated(t *testing.T) {
	db, _ := persistDB(t)
	m := persistMgr(t, db, nil)
	_ = m.StoreScript("good@x.test", "main", "keep;")
	_ = m.SetActiveScriptByName("good@x.test", "main")
	_ = db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(scriptsBucket))
		_ = b.Put([]byte("junk@x.test"), []byte("{not json"))
		_ = b.Put([]byte("future@x.test"), []byte(`{"v":99,"scripts":[]}`))
		_ = b.Put([]byte("bad@x.test"), []byte(`{"v":1,"active":"gone","scripts":[{"name":"s","source":"if true {"},{"name":"ok","source":"keep;"}]}`))
		return nil
	})
	var warns []string
	m2 := persistMgr(t, db, &warns)
	if !m2.HasActiveScript("good@x.test") {
		t.Fatal("good user lost to a corrupt neighbour")
	}
	if m2.HasActiveScript("junk@x.test") || m2.HasActiveScript("future@x.test") {
		t.Fatal("corrupt record became active")
	}
	if got := m2.ListScripts("bad@x.test"); len(got) != 1 || got[0] != "ok" || m2.HasActiveScript("bad@x.test") {
		t.Fatalf("bad: scripts=%v active=%v", got, m2.HasActiveScript("bad@x.test"))
	}
	if len(warns) < 4 {
		t.Fatalf("warnings = %v, want one per problem", warns)
	}
}

func TestSievePersistFailureRollsBack(t *testing.T) {
	db, _ := persistDB(t)
	m := persistMgr(t, db, nil)
	_ = m.StoreScript("a@x.test", "main", "keep;")
	_ = m.SetActiveScriptByName("a@x.test", "main")
	_ = db.Close() // every later save fails
	if err := m.storeScript("a@x.test", "new", "keep;", true); err == nil || !strings.Contains(err.Error(), errScriptStorage.Error()) {
		t.Fatalf("store err = %v", err)
	}
	if len(m.ListScripts("a@x.test")) != 1 {
		t.Fatalf("memory ran ahead of disk: %v", m.ListScripts("a@x.test"))
	}
	if err := m.deactivateScript("a@x.test"); err == nil || m.GetActiveScriptName("a@x.test") != "main" {
		t.Fatalf("deactivate not rolled back: %v %q", err, m.GetActiveScriptName("a@x.test"))
	}
	if err := m.StoreScript("c@x.test", "s", "keep;"); err == nil || len(m.ListScripts("c@x.test")) != 0 {
		t.Fatalf("new user not rolled back: %v %v", err, m.ListScripts("c@x.test"))
	}
}

func TestSievePersistQuotaSurvivesReload(t *testing.T) {
	db, _ := persistDB(t)
	m := persistMgr(t, db, nil)
	for i := 0; i < maxScriptsPerUser; i++ {
		if err := m.storeScript("a@x.test", "s"+string(rune('A'+i%26))+strings.Repeat("x", i/26), "keep;", true); err != nil {
			t.Fatalf("store %d: %v", i, err)
		}
	}
	m2 := persistMgr(t, db, nil)
	if err := m2.storeScript("a@x.test", "overflow", "keep;", true); err != errQuotaMaxScripts {
		t.Fatalf("quota after reload: %v", err)
	}
}

func TestSievePersistAttachIdempotent(t *testing.T) {
	db, _ := persistDB(t)
	m := persistMgr(t, db, nil)
	_ = m.StoreScript("a@x.test", "main", "keep;")
	if err := m.AttachStore(db, nil); err != nil || !m.StoreAttached() {
		t.Fatalf("second attach: %v", err)
	}
	if len(m.ListScripts("a@x.test")) != 1 {
		t.Fatal("scripts changed by re-attach")
	}
	if err := NewManager().AttachStore(nil, nil); err == nil {
		t.Fatal("nil store accepted")
	}
}

func TestSievePersistUserResolver(t *testing.T) {
	srv := NewManageSieveServer(NewManager(), nil)
	if got := srv.sessionUser("jdoe"); got != "jdoe" {
		t.Fatalf("no resolver: %q", got)
	}
	srv.SetUserResolver(func(l string) string {
		if l == "jdoe" {
			return "jdoe@x.test"
		}
		return ""
	})
	if got := srv.sessionUser("jdoe"); got != "jdoe@x.test" {
		t.Fatalf("resolved: %q", got)
	}
	if got := srv.sessionUser("other"); got != "other" {
		t.Fatalf("empty resolution must keep the login: %q", got)
	}
}

func preLoginExchange(t *testing.T, payload string) []string {
	t.Helper()
	srv := NewManageSieveServer(NewManager(), nil)
	client, server := net.Pipe()
	srv.wg.Add(1)
	go srv.handleConn(server)
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	r := bufio.NewReader(client)
	if l, err := readManageSieveGreeting(r); err != nil || !strings.HasPrefix(l, "OK") {
		t.Fatalf("greeting %q %v", l, err)
	}
	go func() { _, _ = client.Write([]byte(payload)) }()
	var out []string
	for {
		l, err := r.ReadString('\n')
		if err != nil {
			return out
		}
		out = append(out, strings.TrimSpace(l))
	}
}

func TestSievePersistPreLoginLiteralNotParsedAsCommands(t *testing.T) {
	// Control: pre-login commands without a literal keep the session.
	if got := preLoginExchange(t, "NOOP\r\nLOGOUT\r\n"); len(got) != 2 || !strings.HasPrefix(got[0], "OK") {
		t.Fatalf("control: %q", got)
	}
	for _, p := range []string{
		"PUTSCRIPT \"x\" {6+}\r\nLOGOUT\r\n",
		"PUTSCRIPT \"x\" {6}\r\nLOGOUT\r\n",
		"PUTSCRIPT \"x\" 6\r\nLOGOUT\r\n",
		"CHECKSCRIPT {6+}\r\nLOGOUT\r\n",
		"CHECKSCRIPT 6\r\nLOGOUT\r\n",
	} {
		got := preLoginExchange(t, p)
		if len(got) != 1 || !strings.HasPrefix(got[0], "NO") {
			t.Errorf("%q: got %q, want a single NO then close", strings.SplitN(p, "\r\n", 2)[0], got)
		}
	}
	// A quoted script has nothing following it: the session stays usable.
	got := preLoginExchange(t, "CHECKSCRIPT \"keep;\"\r\nNOOP\r\nLOGOUT\r\n")
	if len(got) != 3 || !strings.HasPrefix(got[0], "NO") || !strings.HasPrefix(got[1], "OK") {
		t.Errorf("quoted pre-login: %q", got)
	}
}
