package cli

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/caldav"
	"github.com/umailserver/umailserver/internal/carddav"
	"github.com/umailserver/umailserver/internal/config"
	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/push"
	"github.com/umailserver/umailserver/internal/queue"
	"github.com/umailserver/umailserver/internal/vacation"
)

// r74CopyTree performs a printed restore step: copy src/* into dst.
func r74CopyTree(t *testing.T, src, dst string) {
	t.Helper()
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return
	}
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestBackupRestoresAllDataStores (F5550): every store the server keeps under
// DataDir must survive Backup -> Restore -> the printed copy steps. Before the
// fix queue/, caldav/, carddav/, push/, vacation/ and dkim/ were not archived.
func TestBackupRestoresAllDataStores(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	root := t.TempDir()
	src := filepath.Join(root, "site", "data")
	if err := os.MkdirAll(src, 0o750); err != nil {
		t.Fatal(err)
	}
	const user = "alice@example.com"

	adb, err := db.Open(filepath.Join(src, "umailserver.db"))
	if err != nil {
		t.Fatal(err)
	}
	qid, err := queue.NewManager(adb, nil, filepath.Join(src, "queue"), logger).
		Enqueue("a@example.com", []string{"b@example.org"}, []byte("Subject: pending\r\n\r\nbody\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := adb.Close(); err != nil {
		t.Fatal(err)
	}
	cal := caldav.NewStorage(filepath.Join(src, "caldav"))
	if err := cal.CreateCalendar(user, &caldav.Calendar{ID: "work", Name: "Work"}); err != nil {
		t.Fatal(err)
	}
	if err := cal.SaveEvent(user, "work", &caldav.CalendarEvent{UID: "ev1", Summary: "s", Start: time.Unix(0, 0)}, "BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n"); err != nil {
		t.Fatal(err)
	}
	card := carddav.NewStorage(filepath.Join(src, "carddav"))
	if err := card.CreateAddressbook(user, &carddav.Addressbook{ID: "default", Name: "Contacts"}); err != nil {
		t.Fatal(err)
	}
	if err := card.SaveContact(user, "default", &carddav.Contact{UID: "c1", FullName: "Bob"}, "BEGIN:VCARD\r\nEND:VCARD\r\n"); err != nil {
		t.Fatal(err)
	}
	ps, err := push.NewServiceWithConfig(filepath.Join(src, "push"), push.Config{}, logger)
	if err != nil {
		t.Fatal(err)
	}
	vapid := ps.GetVAPIDPublicKey()
	if err := vacation.NewManager(filepath.Join(src, "vacation"), logger).
		SetConfig(user, &vacation.Config{Enabled: true, Subject: "away", Message: "back soon"}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "dkim"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "dkim", "example.com.private.pem"), []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{}
	cfg.Server.DataDir = src
	bm := NewBackupManager(cfg)
	backups := filepath.Join(root, "backups")
	if err := bm.Backup(backups); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(backups)
	if err != nil || len(entries) != 1 {
		t.Fatalf("backup dir: %v %d", err, len(entries))
	}
	archive := filepath.Join(backups, entries[0].Name())
	if err := bm.Verify(archive); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if err := bm.Restore(archive); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(root, "restored")
	restoreTemp := filepath.Join(src, "..", "restore_temp")
	for _, part := range []string{"config", "database", "messages"} {
		r74CopyTree(t, filepath.Join(restoreTemp, part), dst)
	}

	if _, err := os.Stat(filepath.Join(dst, "queue", "queue", qid+".msg")); err != nil {
		t.Errorf("queued message body not restored: %v", err)
	}
	// GetEvent/GetContact return ("", nil) for a missing item.
	if d, err := caldav.NewStorage(filepath.Join(dst, "caldav")).GetEvent(user, "work", "ev1"); err != nil || d == "" {
		t.Errorf("calendar event not restored: %q %v", d, err)
	}
	if d, err := carddav.NewStorage(filepath.Join(dst, "carddav")).GetContact(user, "default", "c1"); err != nil || d == "" {
		t.Errorf("contact not restored: %q %v", d, err)
	}
	if ps2, err := push.NewServiceWithConfig(filepath.Join(dst, "push"), push.Config{}, logger); err != nil || ps2.GetVAPIDPublicKey() != vapid {
		t.Errorf("VAPID key pair not restored (err=%v)", err)
	}
	if c, err := vacation.NewManager(filepath.Join(dst, "vacation"), logger).GetConfig(user); err != nil || c == nil || !c.Enabled {
		t.Errorf("vacation settings not restored: %+v %v", c, err)
	}
	if _, err := os.Stat(filepath.Join(dst, "dkim", "example.com.private.pem")); err != nil {
		t.Errorf("DKIM key not restored: %v", err)
	}
}
