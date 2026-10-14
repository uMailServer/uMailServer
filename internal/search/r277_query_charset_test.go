package search

import "testing"

// F7510: only from/to/subject/has are query operators; "10:30" and URLs are text.
func TestR277UnknownColonIsText(t *testing.T) {
	idx := NewIndex()
	idx.Add(&Document{ID: "inbox:1", Content: "meeting at 10:30 see http://example.com/agenda", Fields: map[string]string{"subject": "Schedule", "from": "a@b.org"}})
	idx.Add(&Document{ID: "inbox:2", Content: "other", Fields: map[string]string{"subject": "nothing"}})
	for _, q := range []string{"10:30", "http://example.com/agenda", "MEETING 10:30"} {
		if r := idx.Search(q, SearchOptions{}); len(r) == 0 || r[0].DocID != "inbox:1" {
			t.Errorf("query %q: %v", q, r)
		}
	}
	if r := idx.Search("subject:schedule", SearchOptions{}); len(r) != 1 {
		t.Errorf("known field: %v", r)
	}
	if r := idx.Search("Subject:schedule 10:30", SearchOptions{}); len(r) != 1 {
		t.Errorf("mixed: %v", r)
	}
	if r := idx.Search("foo:", SearchOptions{}); len(r) != 0 {
		t.Errorf("hostile: %v", r)
	}
}

// F7511: ISO-8859-9 / windows-1254 (Turkish) bodies and RFC 2047 words decode.
func TestR277TurkishCharset(t *testing.T) {
	for _, cs := range []string{"iso-8859-9", "ISO-8859-9", "windows-1254"} {
		got := extractTextContent([]byte("Content-Type: text/plain; charset=" + cs + "\r\n\r\n\xfd\xfe\xfdk \xdd\xde\xd0 \xf0\r\n"))
		if got != "ışık İŞĞ ğ" {
			t.Errorf("%s: %q", cs, got)
		}
	}
	if d := decodeHeaderValue("=?iso-8859-9?Q?=DD=DE=FD=FE?="); d != "İŞış" {
		t.Errorf("header: %q", d)
	}
	if got := extractTextContent([]byte("Content-Type: text/plain; charset=iso-8859-1\r\n\r\ncaf\xe9\r\n")); got != "café" {
		t.Errorf("latin1 control: %q", got)
	}
}
