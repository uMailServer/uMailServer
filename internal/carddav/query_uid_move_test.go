// Regression tests for Round 76 F5573 (UID property with parameters or a
// lowercase name) and F5574 (MOVE/COPY status 201 vs 204).

package carddav

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// ---- F5573: UID property with parameters / lowercase name not recognised ----

func q76UIDLines(data string) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(data, "\r\n", "\n"), "\n") {
		name := strings.SplitN(strings.SplitN(l, ":", 2)[0], ";", 2)[0]
		if strings.EqualFold(name, "UID") {
			out = append(out, l)
		}
	}
	return out
}

func TestCardDAVQueryF5573Control(t *testing.T) {
	s := q76Setup(t)
	w := q76Do(s, q76User, "MOVE", "/dav/addressbooks/ab1/c1.vcf", "", map[string]string{"Destination": "/dav/addressbooks/ab1/c9.vcf"})
	d, _ := s.storage.GetContact(q76User, "ab1", "c9")
	t.Logf("CONTROL EXPECTED: MOVE c1->c9 rewrites plain UID ACTUAL: %d %v", w.Code, q76UIDLines(d))
	if w.Code >= 300 || fmt.Sprint(q76UIDLines(d)) != "[UID:c9]" {
		t.Fatal("invalid control")
	}
}

func TestCardDAVQueryF5573Failure(t *testing.T) {
	s := q76Setup(t)
	put := q76Do(s, q76User, "PUT", "/dav/addressbooks/ab1/c5.vcf", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID;VALUE=text:c5\r\nFN:Param Uid\r\nEND:VCARD\r\n", nil)
	stored, _ := s.storage.GetContact(q76User, "ab1", "c5")
	mv := q76Do(s, q76User, "MOVE", "/dav/addressbooks/ab1/c5.vcf", "", map[string]string{"Destination": "/dav/addressbooks/ab1/c6.vcf"})
	moved, _ := s.storage.GetContact(q76User, "ab1", "c6")
	t.Logf("EXPECTED: PUT keeps one UID [UID;VALUE=text:c5]; MOVE rewrites it to [UID;VALUE=text:c6] ACTUAL: put=%d stored=%v move=%d moved=%v", put.Code, q76UIDLines(stored), mv.Code, q76UIDLines(moved))
	if fmt.Sprint(q76UIDLines(stored)) != "[UID;VALUE=text:c5]" || fmt.Sprint(q76UIDLines(moved)) != "[UID;VALUE=text:c6]" {
		t.Fatal("DEFECT F5573: UID with parameters is not recognised")
	}
}

func TestCardDAVQueryF5573Edges(t *testing.T) {
	s := q76Setup(t)
	// Lowercase property name seeded directly in storage must still be listed.
	_ = s.storage.SaveContact(q76User, "ab1", &Contact{UID: "c7"}, "BEGIN:VCARD\nVERSION:3.0\nuid:c7\nFN:Lower\nEND:VCARD\n")
	code, hrefs, _ := q76Report(s, q76Query(""))
	listed := strings.Contains(fmt.Sprint(hrefs), "/dav/addressbooks/ab1/c7.vcf")
	t.Logf("lowercase EXPECTED: listed ACTUAL: %d listed=%v", code, listed)
	if !listed {
		t.Error("DEFECT F5573 edge lowercase uid not listed")
	}
	// COPY with rename rewrites a lowercase UID too and leaves no stale UID.
	w := q76Do(s, q76User, "COPY", "/dav/addressbooks/ab1/c7.vcf", "", map[string]string{"Destination": "/dav/addressbooks/ab2/c8.vcf"})
	copied, _ := s.storage.GetContact(q76User, "ab2", "c8")
	t.Logf("copy EXPECTED: [uid:c8] ACTUAL: %d %v", w.Code, q76UIDLines(copied))
	if fmt.Sprint(q76UIDLines(copied)) != "[uid:c8]" {
		t.Error("DEFECT F5573 edge copy lowercase")
	}
	// A NOTE that mentions the old UID text is not rewritten.
	_ = q76Do(s, q76User, "PUT", "/dav/addressbooks/ab1/n1.vcf", "BEGIN:VCARD\r\nVERSION:3.0\r\nNOTE:UID:n1 was here\r\nUID:n1\r\nFN:Note\r\nEND:VCARD\r\n", nil)
	_ = q76Do(s, q76User, "MOVE", "/dav/addressbooks/ab1/n1.vcf", "", map[string]string{"Destination": "/dav/addressbooks/ab1/n2.vcf"})
	n2, _ := s.storage.GetContact(q76User, "ab1", "n2")
	t.Logf("note EXPECTED: NOTE kept, [UID:n2] ACTUAL: note=%v %v", strings.Contains(n2, "NOTE:UID:n1 was here"), q76UIDLines(n2))
	if !strings.Contains(n2, "NOTE:UID:n1 was here") || fmt.Sprint(q76UIDLines(n2)) != "[UID:n2]" {
		t.Error("DEFECT F5573 edge note")
	}
	// Legacy "UID=" spelling keeps working for extraction.
	if got := s.extractUIDFromVCard("BEGIN:VCARD\nUID=legacy\nEND:VCARD"); got != "legacy" {
		t.Errorf("DEFECT F5573 edge legacy got %q", got)
	}
}

// ---- F5574: MOVE/COPY to a new destination answer 204 instead of 201 ----

func TestCardDAVQueryF5574Control(t *testing.T) {
	s := q76Setup(t)
	w := q76Do(s, q76User, "COPY", "/dav/addressbooks/ab1/c1.vcf", "", map[string]string{"Destination": "/dav/addressbooks/ab1/c2.vcf"})
	t.Logf("CONTROL EXPECTED: COPY over existing -> 204 ACTUAL: %d", w.Code)
	if w.Code != http.StatusNoContent {
		t.Fatal("invalid control")
	}
}

func TestCardDAVQueryF5574Failure(t *testing.T) {
	s := q76Setup(t)
	c := q76Do(s, q76User, "COPY", "/dav/addressbooks/ab1/c1.vcf", "", map[string]string{"Destination": "/dav/addressbooks/ab2/new1.vcf"})
	m := q76Do(s, q76User, "MOVE", "/dav/addressbooks/ab1/c2.vcf", "", map[string]string{"Destination": "/dav/addressbooks/ab2/new2.vcf"})
	t.Logf("EXPECTED: COPY/MOVE creating destination -> 201/201 (RFC 4918 §9.8.5, §9.9.4) ACTUAL: %d/%d", c.Code, m.Code)
	if c.Code != http.StatusCreated || m.Code != http.StatusCreated {
		t.Fatal("DEFECT F5574: MOVE/COPY creating a resource answer 204")
	}
}

func TestCardDAVQueryF5574Edges(t *testing.T) {
	s := q76Setup(t)
	m := q76Do(s, q76User, "MOVE", "/dav/addressbooks/ab1/c1.vcf", "", map[string]string{"Destination": "/dav/addressbooks/ab1/c2.vcf"})
	t.Logf("MOVE over existing EXPECTED: 204 ACTUAL: %d", m.Code)
	if m.Code != http.StatusNoContent {
		t.Error("DEFECT F5574 edge move overwrite")
	}
	c := q76Do(s, q76User, "COPY", "/dav/addressbooks/ab1/c3.vcf", "", map[string]string{"Destination": "/dav/addressbooks/ab1/c3b.vcf"})
	c2 := q76Do(s, q76User, "COPY", "/dav/addressbooks/ab1/c3.vcf", "", map[string]string{"Destination": "/dav/addressbooks/ab1/c3b.vcf"})
	t.Logf("repeat COPY EXPECTED: 201 then 204 ACTUAL: %d then %d", c.Code, c2.Code)
	if c.Code != http.StatusCreated || c2.Code != http.StatusNoContent {
		t.Error("DEFECT F5574 edge repeat copy")
	}
	f := q76Do(s, q76User, "COPY", "/dav/addressbooks/ab1/c3.vcf", "", map[string]string{"Destination": "/dav/addressbooks/ab1/c3c.vcf", "Overwrite": "F"})
	t.Logf("Overwrite F new EXPECTED: 201 ACTUAL: %d", f.Code)
	if f.Code != http.StatusCreated {
		t.Error("DEFECT F5574 edge overwrite F")
	}
}
