package jmap

// Email/set creation wrote header and body text as given:
// F7771: a display name ending in "\" escaped the closing quote and broke
// the whole To header; F7772: non-ASCII subjects and names were raw 8-bit
// header bytes; F7773: body lines over 998 octets were sent as 8bit.

import (
	"io"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

func buildForTest(t *testing.T, data map[string]interface{}) (string, *mail.Message) {
	t.Helper()
	store, err := storage.NewMessageStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	raw, berr := buildRFC5322Email("u@x.test", data, store)
	if berr != nil {
		t.Fatalf("build: %s", berr.description)
	}
	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("message does not parse: %v", err)
	}
	return string(raw), msg
}

func TestEmailBuildDisplayNameBackslash(t *testing.T) {
	for _, name := range []string{`Smith\`, `a\b, c`, `say "hi"`} {
		_, msg := buildForTest(t, map[string]interface{}{"to": []interface{}{
			map[string]interface{}{"name": name, "email": "a@x.test"},
			map[string]interface{}{"email": "b@x.test"},
		}})
		got, err := msg.Header.AddressList("To")
		if err != nil || len(got) != 2 || got[0].Name != name || got[1].Address != "b@x.test" {
			t.Errorf("name %q: recipients %v, err %v", name, got, err)
		}
	}
}

func TestEmailBuildEncodesNonASCIIHeaders(t *testing.T) {
	raw, msg := buildForTest(t, map[string]interface{}{
		"subject": "Grüße aus Köln",
		"to":      []interface{}{map[string]interface{}{"name": "Jörg Müller", "email": "a@x.test"}},
	})
	head := raw[:strings.Index(raw, "\r\n\r\n")]
	for i := 0; i < len(head); i++ {
		if head[i] >= 0x80 {
			t.Fatalf("raw 8-bit byte in header:\n%s", head)
		}
	}
	dec := new(mime.WordDecoder)
	if subj, err := dec.DecodeHeader(msg.Header.Get("Subject")); err != nil || subj != "Grüße aus Köln" {
		t.Errorf("subject %q, err %v", subj, err)
	}
	to, err := (&mail.AddressParser{WordDecoder: dec}).ParseList(msg.Header.Get("To"))
	if err != nil || len(to) != 1 || to[0].Name != "Jörg Müller" {
		t.Errorf("to %v, err %v", to, err)
	}
}

func TestEmailBuildWrapsLongLines(t *testing.T) {
	for _, n := range []int{998, 999, 3000} {
		value := strings.Repeat("a", n)
		raw, msg := buildForTest(t, map[string]interface{}{
			"textBody":   []interface{}{map[string]interface{}{"partId": "1"}},
			"bodyValues": map[string]interface{}{"1": map[string]interface{}{"value": value}},
		})
		for _, l := range strings.Split(raw, "\r\n") {
			if len(l) > 998 {
				t.Fatalf("n=%d: line of %d octets", n, len(l))
			}
		}
		var r io.Reader = msg.Body
		if cte := msg.Header.Get("Content-Transfer-Encoding"); cte == "quoted-printable" {
			r = quotedprintable.NewReader(r)
		} else if n > 998 || cte != "8bit" {
			t.Fatalf("n=%d: Content-Transfer-Encoding %q", n, cte)
		}
		if body, _ := io.ReadAll(r); string(body) != value {
			t.Errorf("n=%d: decoded body differs", n)
		}
	}
}
