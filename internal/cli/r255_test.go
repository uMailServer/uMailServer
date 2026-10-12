package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/umailserver/umailserver/internal/config"
)

// F7320: the SPF version tag is case-insensitive (RFC 7208 4.5); a record
// published as "V=SPF1 mx -all" must be recognised, not reported missing.
func TestR255SPFVersionCaseInsensitive(t *testing.T) {
	fake := startFakeRBLServer(t, func(qname string, qtype uint16) (int, [][]byte) {
		if qname == "example.com" && qtype == spfTypeTXT {
			return rblRCODENoError, spfTXTAnswer("V=SPF1 MX -all")
		}
		return rblRCODENXDomain, nil
	})
	withFakeRBLResolver(t, fake)
	d := NewDiagnostics(&config.Config{Server: config.ServerConfig{Hostname: "mail.example.com"}})
	if r := d.checkSPF("example.com"); r.Status != "pass" {
		t.Fatalf("case-variant SPF record reported %q: %s", r.Status, r.Message)
	}
}

// F7321: the DKIM check must query the selector configured for the domain,
// not always "default".
func TestR255DKIMUsesConfiguredSelector(t *testing.T) {
	fake := startFakeRBLServer(t, func(qname string, qtype uint16) (int, [][]byte) {
		if qname == "s2024._domainkey.example.com" && qtype == spfTypeTXT {
			return rblRCODENoError, spfTXTAnswer("v=DKIM1; k=rsa; p=QUJD")
		}
		return rblRCODENXDomain, nil
	})
	withFakeRBLResolver(t, fake)
	cfg := &config.Config{Server: config.ServerConfig{Hostname: "mail.example.com"},
		Domains: []config.DomainConfig{{Name: "Example.com", DKIM: config.DomainDKIMConfig{Selector: "s2024"}}}}
	r := NewDiagnostics(cfg).checkDKIM("example.com")
	if r.Status != "pass" {
		t.Fatalf("configured selector ignored: %q %s (%s)", r.Status, r.Message, r.RecordName)
	}
}

// F7322: a member name that merely contains ".." inside a path element
// (not a traversal) must restore; real traversal and symlinks stay refused.
func TestR255RestoreDotDotInsideName(t *testing.T) {
	bm, root := newStagingManager(t)
	bf := filepath.Join(root, "b.tar.gz")
	writeStagingArchive(t, bf, []stagingMember{{"messages/a..b.eml", "x"}}, nil, 0)
	if err := bm.Restore(bf); err != nil {
		t.Fatalf("legit filename rejected: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "restore_temp", "messages", "a..b.eml")); err != nil {
		t.Fatal(err)
	}
}

func TestR255RestoreTraversalAndSymlink(t *testing.T) {
	for _, tc := range []struct {
		name string
		h    tar.Header
		ok   bool
	}{
		{"dotdot", tar.Header{Name: "../evil", Typeflag: tar.TypeReg, Mode: 0o600}, false},
		{"nested", tar.Header{Name: "a/../../evil", Typeflag: tar.TypeReg, Mode: 0o600}, false},
		{"symlink", tar.Header{Name: "messages/ln", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd", Mode: 0o777}, true},
	} {
		bm, root := newStagingManager(t)
		var tb bytes.Buffer
		tw := tar.NewWriter(&tb)
		mf := []byte(`{"version":"1","timestamp":"x","hostname":"h","files":[]}`)
		_ = tw.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0o600, Size: int64(len(mf)), Typeflag: tar.TypeReg})
		_, _ = tw.Write(mf)
		h := tc.h
		_ = tw.WriteHeader(&h)
		_ = tw.Close()
		var gb bytes.Buffer
		gw := gzip.NewWriter(&gb)
		_, _ = gw.Write(tb.Bytes())
		_ = gw.Close()
		bf := filepath.Join(root, "b.tar.gz")
		_ = os.WriteFile(bf, gb.Bytes(), 0o600)
		err := bm.Restore(bf)
		if (err == nil) != tc.ok {
			t.Errorf("%s: err=%v want ok=%v", tc.name, err, tc.ok)
		}
		if _, e := os.Lstat(filepath.Join(root, "restore_temp", "messages", "ln")); e == nil {
			t.Errorf("%s: symlink materialised", tc.name)
		}
	}
}
