package cli

// R275 F7491: a file without any mbox "From " separator line is not an MBOX
// and must not be imported as one garbage message.

import (
	"os"
	"path/filepath"
	"testing"
)

func r275Import(t *testing.T, content string) (error, int) {
	t.Helper()
	mm, base, _ := r250Setup(t)
	f := filepath.Join(t.TempDir(), "bob@example.com.mbox")
	if err := os.WriteFile(f, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	err := mm.MigrateFromMBOX(f)
	n := 0
	_ = filepath.Walk(base, func(p string, i os.FileInfo, e error) error {
		if e == nil && i.Mode().IsRegular() {
			n++
		}
		return nil
	})
	return err, n
}

func TestR275F7491MboxWithoutFromLineRejected(t *testing.T) {
	if err, n := r275Import(t, "plain text, not an mbox\nline two\n"); err == nil || n != 0 {
		t.Fatalf("non-mbox: err=%v stored=%d, want error and 0", err, n)
	}
	// control: genuine mbox with preamble garbage before the first From_ line.
	if err, n := r275Import(t, "\n\nFrom a@b x\nSubject: x\n\nbody\n"); err != nil || n != 1 {
		t.Fatalf("mbox: err=%v stored=%d, want nil and 1", err, n)
	}
	// empty and whitespace-only files import nothing without error.
	for _, c := range []string{"", "\n\n"} {
		if err, n := r275Import(t, c); err != nil || n != 0 {
			t.Fatalf("blank %q: err=%v stored=%d", c, err, n)
		}
	}
}
