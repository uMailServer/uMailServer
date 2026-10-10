package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/umailserver/umailserver/internal/config"
	"github.com/umailserver/umailserver/internal/storage"
)

// F5450: the live mail store is <DataDir>/mail (message files under
// mail/messages, the mailbox/UID database at mail/mail.db, see server.New).
// Backup must archive both, under "messages/" so the documented restore step
// (copy restore_temp/messages/* into the data directory) puts them back.
func TestBackupIncludesLiveMailStore(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		t.Fatal(err)
	}
	ms, err := storage.NewMessageStore(dataDir + "/mail/messages")
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("Subject: x\r\n\r\nF5450 body\r\n")
	id, err := ms.StoreMessage("alice@example.com", msg)
	if err != nil {
		t.Fatal(err)
	}
	sdb, err := storage.OpenDatabase(dataDir + "/mail/mail.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := sdb.Close(); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "out")
	bm := NewBackupManager(&config.Config{Server: config.ServerConfig{DataDir: dataDir}})
	if err := bm.Backup(out); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	files, _ := filepath.Glob(filepath.Join(out, "*.tar.gz"))
	if len(files) != 1 {
		t.Fatalf("archives = %v", files)
	}
	raw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	gr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gr)
	got := map[string][]byte{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		got[h.Name] = b
	}
	msgName := "messages/mail/messages/alice@example.com/" + id[:2] + "/" + id[2:4] + "/" + id
	if !bytes.Equal(got[msgName], msg) {
		t.Fatalf("message %s not archived; members=%d", msgName, len(got))
	}
	if _, ok := got["messages/mail/mail.db"]; !ok {
		t.Fatal("mail/mail.db not archived")
	}
}

// F5451: Restore and Verify must check the gzip CRC-32 footer. With a
// manifest that lists no file hashes, the footer is the only integrity check.
func TestRestoreRejectsGzipChecksumMismatch(t *testing.T) {
	var tb bytes.Buffer
	tw := tar.NewWriter(&tb)
	for _, m := range []struct{ name, body string }{
		{"database/umailserver.db", "legacy-db"},
		{"manifest.json", `{"version":"1.0.0","timestamp":"t","hostname":"h"}`},
	} {
		if err := tw.WriteHeader(&tar.Header{Name: m.name, Mode: 0o600, Size: int64(len(m.body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(m.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	var gzBuf bytes.Buffer
	gw, _ := gzip.NewWriterLevel(&gzBuf, gzip.NoCompression)
	if _, err := gw.Write(tb.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	intact := gzBuf.Bytes()
	corrupt := append([]byte(nil), intact...)
	corrupt[bytes.Index(corrupt, []byte("legacy-db"))] = 'L'

	for _, tc := range []struct {
		name    string
		archive []byte
		wantErr bool
	}{
		{"intact", intact, false},
		{"corrupt", corrupt, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dataDir := filepath.Join(root, "data")
			if err := os.MkdirAll(dataDir, 0o750); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "b.tar.gz")
			if err := os.WriteFile(path, tc.archive, 0o600); err != nil {
				t.Fatal(err)
			}
			bm := NewBackupManager(&config.Config{Server: config.ServerConfig{DataDir: dataDir}})
			if err := bm.Verify(path); (err != nil) != tc.wantErr {
				t.Fatalf("Verify err = %v, wantErr %v", err, tc.wantErr)
			}
			if err := bm.Restore(path); (err != nil) != tc.wantErr {
				t.Fatalf("Restore err = %v, wantErr %v", err, tc.wantErr)
			}
			_, statErr := os.Stat(filepath.Join(root, "restore_temp"))
			if published := statErr == nil; published == tc.wantErr {
				t.Fatalf("restore_temp published = %v, wantErr %v", published, tc.wantErr)
			}
		})
	}
}
