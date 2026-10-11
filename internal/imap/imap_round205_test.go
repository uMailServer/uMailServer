package imap

import (
	"strings"
	"testing"
	"time"
)

// F6820: LIST "%" must reveal parents that exist only implicitly.
func TestF6820_ListShowsImplicitParents(t *testing.T) {
	ms, user := r183Setup(t, 0)
	c := newR183(t, ms, user)
	c.cmd(`CREATE "a/b/c"`)
	out := joined(c.cmd(`LIST "" %`))
	if !strings.Contains(out, `* LIST (\Noselect \HasChildren) "/" "a"`) {
		t.Fatalf("missing parent a: %q", out)
	}
	out = joined(c.cmd(`LIST "" "a/%"`))
	if !strings.Contains(out, `"a/b"`) || strings.Contains(out, `"a/b/c"`) {
		t.Fatalf("a/%% wrong: %q", out)
	}
	out = joined(c.cmd(`LIST "" *`))
	for _, n := range []string{`"a"`, `"a/b"`, `"a/b/c"`} {
		if !strings.Contains(out, n) {
			t.Fatalf("* missing %s: %q", n, out)
		}
	}
}

// F6824: INBOX is case-insensitive in LIST/LSUB patterns (RFC 3501 §5.1).
func TestF6824_ListInboxCaseInsensitive(t *testing.T) {
	ms, user := r183Setup(t, 0)
	c := newR183(t, ms, user)
	if out := joined(c.cmd(`LIST "" inbox`)); !strings.Contains(out, `"INBOX"`) {
		t.Fatalf("LIST inbox: %q", out)
	}
	c.cmd(`SUBSCRIBE INBOX`)
	if out := joined(c.cmd(`LSUB "" InBoX`)); !strings.Contains(out, `"INBOX"`) {
		t.Fatalf("LSUB InBoX: %q", out)
	}
}

// F6821: only the pre-auth state uses the short default read timeout; an
// authenticated session gets the RFC 3501 §5.4 30-minute autologout.
func TestF6821_AuthenticatedAutologoutAtLeast30Min(t *testing.T) {
	srv := NewServer(&Config{Addr: ":0"}, &mockMailstore{})
	s := &Session{server: srv, state: StateNotAuthenticated}
	if got := s.readDeadlineWindow(); got != 30*time.Second {
		t.Fatalf("pre-auth window %v", got)
	}
	s.state = StateAuthenticated
	if got := s.readDeadlineWindow(); got < 30*time.Minute {
		t.Fatalf("authenticated window %v < 30m", got)
	}
	s.state = StateSelected
	if got := s.readDeadlineWindow(); got < 30*time.Minute {
		t.Fatalf("selected window %v < 30m", got)
	}
	srv.SetReadTimeout(2 * time.Second) // operator choice is honoured
	if got := s.readDeadlineWindow(); got != 2*time.Second {
		t.Fatalf("explicit window %v", got)
	}
}

// F6822: a multipart without boundary must not yield a grammar-invalid
// "MULTIPART" leaf.
func TestF6822_BodyStructureMultipartWithoutBoundary(t *testing.T) {
	bs := bodyStructure([]byte("Content-Type: multipart/mixed\r\n\r\nnoboundary\r\n"), true)
	if strings.Contains(bs, "MULTIPART") || !strings.HasPrefix(bs, `("TEXT" "PLAIN"`) {
		t.Fatalf("got %s", bs)
	}
}

// F6823: a mailbox name containing "+}" must not be read as a LITERAL+ marker.
func TestF6823_AppendSyncLiteralGetsContinuationWithPlusBraceName(t *testing.T) {
	ms, user := r183Setup(t, 0)
	if err := ms.CreateMailbox(user, "x+}"); err != nil {
		t.Skipf("name not creatable: %v", err)
	}
	c := newR183(t, ms, user)
	c.send(`ta APPEND "x+}" {5}`)
	l, err := c.r.ReadString('\n')
	if err != nil || !strings.HasPrefix(l, "+") {
		t.Fatalf("no continuation: %q %v", l, err)
	}
	c.send("hello")
	if out := joined(c.until("ta")); !strings.Contains(out, "OK") {
		t.Fatalf("append: %q", out)
	}
}

// F6825: bare ENABLE is a syntax error (RFC 5161).
func TestF6825_EnableRequiresCapability(t *testing.T) {
	ms, user := r183Setup(t, 0)
	c := newR183(t, ms, user)
	if out := joined(c.cmd("ENABLE")); !strings.Contains(out, " BAD ") {
		t.Fatalf("got %q", out)
	}
}
