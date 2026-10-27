package carddav

import (
	"reflect"
	"strings"
	"testing"
)

func TestCardRoundTrip(t *testing.T) {
	c := Card{
		UID:          "u1",
		FullName:     "Doe, Jane; Jr",
		Emails:       []string{"jane@example.com", "j@work.example"},
		Phones:       []string{"+1 555 0100"},
		Organization: "Acme; Inc",
		Note:         "line one\nline two " + strings.Repeat("é", 60),
	}
	data := NewCardVCard(c)
	for _, line := range strings.Split(data, "\r\n") {
		if len(line) > 75 {
			t.Fatalf("line not folded: %q", line)
		}
	}
	if got := ParseCard(data); !reflect.DeepEqual(got, c) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", got, c)
	}
}

// Saving from the webmail keeps the properties it does not edit.
func TestMergeCardKeepsOtherProperties(t *testing.T) {
	stored := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:u2\r\nN:Doe;John;;;\r\nFN:John Doe\r\n" +
		"EMAIL;TYPE=HOME:old@example.com\r\nitem1.TEL:+1 555 0199\r\n" +
		"PHOTO;ENCODING=b;TYPE=JPEG:" + strings.Repeat("A", 100) + "\r\n" +
		"ADR;TYPE=HOME:;;1 Main St;Town;;12345;US\r\nBDAY:1990-01-02\r\nEND:VCARD\r\n"
	merged := MergeCard(stored, Card{UID: "u2", FullName: "John Q. Doe", Emails: []string{"new@example.com"}})

	got := ParseCard(merged)
	if got.FullName != "John Q. Doe" || !reflect.DeepEqual(got.Emails, []string{"new@example.com"}) || len(got.Phones) != 0 {
		t.Fatalf("edited fields not applied: %+v", got)
	}
	unfolded := strings.Join(unfoldVCard(merged), "\n")
	for _, keep := range []string{"N:Doe;John;;;", "PHOTO;ENCODING=b;TYPE=JPEG:" + strings.Repeat("A", 100), "ADR;TYPE=HOME:;;1 Main St;Town;;12345;US", "BDAY:1990-01-02", "UID:u2"} {
		if !strings.Contains(unfolded, keep) {
			t.Errorf("lost %q:\n%s", keep, merged)
		}
	}
	if strings.Contains(merged, "old@example.com") || !strings.HasSuffix(merged, "END:VCARD\r\n") {
		t.Errorf("merged vCard:\n%s", merged)
	}
}

func TestParseCardNameFromN(t *testing.T) {
	c := ParseCard("BEGIN:VCARD\nVERSION:4.0\nN:Smith;Anna;Maria;Dr.;\nEMAIL:mailto:anna@example.com\nEND:VCARD\n")
	if c.FullName != "Dr. Anna Maria Smith" || !reflect.DeepEqual(c.Emails, []string{"anna@example.com"}) {
		t.Fatalf("%+v", c)
	}
}
