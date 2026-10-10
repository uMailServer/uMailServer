package cli

// Regression tests for migration defects found in audit round 69
// (internal/cli/migrate.go).
//
// F5500: MBOX import filed each message under the address in its From:
// header (the author), so a user's mailbox export landed in the senders'
// mailboxes and any sender could choose a local mailbox for the message.
// The target is now the MBOX file name.
//
// F5502: IMAP migration requested FETCH BODY, which returns the body
// structure only; no message content was ever stored while the migration
// reported success. It now fetches BODY.PEEK[].
//
// F5504: Dovecot user import created accounts on domains not hosted here and
// beyond the domain's max_accounts, rules the admin API enforces.

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emersion/go-imap/client"
	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/storage"
)

func mig69Stored(t *testing.T, base, user string) []string {
	t.Helper()
	var out []string
	root := filepath.Join(base, user)
	if _, err := os.Stat(root); err != nil {
		return nil
	}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || strings.HasPrefix(info.Name(), ".tmp") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out = append(out, string(b))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

func mig69Store(t *testing.T) (*storage.MessageStore, string) {
	t.Helper()
	base := filepath.Join(t.TempDir(), "store")
	ms, err := storage.NewMessageStore(base)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	return ms, base
}

func TestMigrateMBOXRoutesByFileNotSenderHeader(t *testing.T) {
	ms, base := mig69Store(t)
	mm := NewMigrationManager(nil, ms, nil)
	p := filepath.Join(t.TempDir(), "alice@example.test.mbox")
	content := "From mallory@evil.test Mon Jan  1 00:00:00 2024\n" +
		"From: Admin <postmaster@example.test>\nSubject: phish\n\nclick\n\n" +
		"From x@y Mon Jan  1 00:00:00 2024\nSubject: plain\n\nhello\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mm.importMBOXFile(p); err != nil {
		t.Fatalf("import: %v", err)
	}
	if n := len(mig69Stored(t, base, "alice@example.test")); n != 2 {
		t.Errorf("alice@example.test has %d messages, want 2", n)
	}
	if n := len(mig69Stored(t, base, "postmaster@example.test")); n != 0 {
		t.Errorf("postmaster@example.test got %d messages chosen by the From: header", n)
	}
}

const mig69Msg = "From: contact@example.org\r\nTo: dave@example.test\r\nSubject: hi\r\n\r\nHi there :)"

// mig69IMAPServer is a minimal scripted IMAP4rev1 server with one INBOX
// message. FETCH BODY (no section) gets a body structure, as RFC 3501 6.4.5
// specifies; BODY[] / BODY.PEEK[] gets the content.
func mig69IMAPServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go mig69ServeIMAP(conn)
		}
	}()
	return ln.Addr().String()
}

func mig69ServeIMAP(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	w := func(s string) { _, _ = io.WriteString(conn, s+"\r\n") }
	w("* OK [CAPABILITY IMAP4rev1] fake ready")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		f := strings.SplitN(strings.TrimRight(line, "\r\n"), " ", 3)
		if len(f) < 2 {
			continue
		}
		tag, cmd, args := f[0], strings.ToUpper(f[1]), ""
		if len(f) == 3 {
			args = strings.ToUpper(f[2])
		}
		switch cmd {
		case "CAPABILITY":
			w("* CAPABILITY IMAP4rev1")
			w(tag + " OK done")
		case "LOGIN":
			w(tag + " OK logged in")
		case "LIST":
			w(`* LIST () "/" INBOX`)
			w(tag + " OK done")
		case "SELECT", "EXAMINE":
			w(`* FLAGS (\Seen)`)
			w("* 1 EXISTS")
			w("* 0 RECENT")
			w("* OK [UIDVALIDITY 1] ok")
			w(tag + " OK [READ-WRITE] done")
		case "FETCH":
			items := []string{`FLAGS (\Seen)`}
			if strings.Contains(args, "BODY[]") || strings.Contains(args, "BODY.PEEK[]") {
				items = append(items, fmt.Sprintf("BODY[] {%d}\r\n%s", len(mig69Msg), mig69Msg))
			} else if strings.Contains(args, "BODY") {
				items = append(items, `BODY ("TEXT" "PLAIN" ("CHARSET" "us-ascii") NIL NIL "7BIT" 11 1)`)
			}
			w("* 1 FETCH (" + strings.Join(items, " ") + ")")
			w(tag + " OK done")
		case "LOGOUT":
			w("* BYE bye")
			w(tag + " OK done")
			return
		default:
			w(tag + " BAD unsupported")
		}
	}
}

func TestMigrateIMAPStoresMessageContent(t *testing.T) {
	addr := mig69IMAPServer(t)
	ms, base := mig69Store(t)
	mm := NewMigrationManager(nil, ms, nil)
	mm.dialIMAP = func(string, bool) (*client.Client, error) { return client.Dial(addr) }
	err := mm.MigrateFromIMAP(MigrateOptions{
		SourceURL:  "imaps://imap.example.test",
		Username:   "username",
		Password:   "password",
		TargetUser: "dave@example.test",
	})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	msgs := mig69Stored(t, base, "dave@example.test")
	if len(msgs) != 1 || msgs[0] != mig69Msg {
		t.Errorf("stored %q, want the one source message", msgs)
	}
}

func TestMigrateDovecotUsersEnforceDomainRules(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.CreateDomain(&db.DomainData{Name: "two.test", MaxAccounts: 2, IsActive: true}); err != nil {
		t.Fatal(err)
	}
	passwd := filepath.Join(t.TempDir(), "passwd")
	lines := "a@two.test:{X}h:1:1::/h:/s\nb@two.test:{X}h:1:1::/h:/s\nc@two.test:{X}h:1:1::/h:/s\n" +
		"x@unhosted.test:{X}h:1:1::/h:/s\nbare:{X}h:1:1::/h:/s\n"
	if err := os.WriteFile(passwd, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	mm := NewMigrationManager(d, nil, nil)
	if err := mm.importDovecotUsers(passwd); err != nil {
		t.Fatalf("import: %v", err)
	}
	for domain, want := range map[string]int{"two.test": 2, "unhosted.test": 0, "": 0} {
		got, err := d.ListAccountsByDomain(domain)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != want {
			t.Errorf("domain %q: %d accounts, want %d", domain, len(got), want)
		}
	}
}
