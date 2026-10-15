package mimeutil

import (
	"reflect"
	"strings"
	"testing"
)

func TestDecodeHeader(t *testing.T) {
	cases := map[string]string{
		"plain ascii": "plain ascii",
		"":            "",
		"=?UTF-8?B?SG/Fn2dlbGRpbml6IOKAkyBUw7xya2nDp2Ugb2xhcmFr?=": "Hoşgeldiniz – Türkiçe olarak",
		"=?utf-8?q?Ekli_rapor_=E2=80=93_=C3=A7ok_g=C3=BCzel?=":     "Ekli rapor – çok güzel",
		"=?iso-8859-9?Q?=DD=DE=FD=FE?=":                            "İŞış",
		"=?windows-1254?Q?=D0=F0?=":                                "Ğğ",
		"=?windows-1252?Q?=80_=93q=94?=":                           "€ “q”",
		"=?iso-8859-1?Q?caf=E9?=":                                  "café",
		"a =?UTF-8?Q?b?= =?UTF-8?Q?c?= d":                          "a bc d",                    // adjacent words join
		"=?x-unknown-charset?Q?abc?=":                              "abc",                       // ASCII payload of an unknown charset is readable
		"=?gb2312?Q?=C4=E3=BA=C3?=":                                "=?gb2312?Q?=C4=E3=BA=C3?=", // unsupported multibyte charset stays encoded, not garbled
		"=?UTF-8?B?!!!notbase64?=":                                 "=?UTF-8?B?!!!notbase64?=",
	}
	for in, want := range cases {
		if got := DecodeHeader(in); got != want {
			t.Errorf("DecodeHeader(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseAddresses(t *testing.T) {
	got, ok := ParseAddresses(`=?UTF-8?B?QXnFn2UgWcSxbG1heg==?= <ayse@x.test>, "Smoke, Admin" <admin@x.test>, bare@x.test`)
	want := []Address{{"Ayşe Yılmaz", "ayse@x.test"}, {"Smoke, Admin", "admin@x.test"}, {"", "bare@x.test"}}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseAddresses = %#v ok=%v, want %#v", got, ok, want)
	}
	if got, ok := ParseAddresses("  "); !ok || got != nil {
		t.Errorf("empty list = %#v %v, want nil, true", got, ok)
	}
	if _, ok := ParseAddresses("not an address list <<"); ok {
		t.Error("invalid list reported ok")
	}
	// a Turkish charset the standard library does not know still decodes
	got, ok = ParseAddresses("=?iso-8859-9?Q?=DD=FEIK?= <a@x.test>")
	if !ok || len(got) != 1 || got[0].Name != "İşIK" {
		t.Errorf("latin5 display name = %#v %v", got, ok)
	}
}

func TestDisplayList(t *testing.T) {
	got := DisplayList(`=?UTF-8?B?QXnFn2UgWcSxbG1heg==?= <ayse@x.test>, "Smoke, Admin" <admin@x.test>, bare@x.test`)
	want := []string{"Ayşe Yılmaz <ayse@x.test>", `"Smoke, Admin" <admin@x.test>`, "bare@x.test"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DisplayList = %#v, want %#v", got, want)
	}
	if DisplayList("") != nil {
		t.Error("empty value must give nil")
	}
	// Unparseable: one decoded element, never split on the comma inside a quoted name.
	if got := DisplayList(`"Unclosed, Name <a@x.test>`); len(got) != 1 {
		t.Errorf("unparseable list was split: %#v", got)
	}
}

// One display name in a charset we cannot convert must not make the whole
// address list unparseable: that collapsed a To list into one element and
// dropped every recipient but the first from a reply-all (review finding).
func TestAddressListSurvivesUnsupportedCharsetName(t *testing.T) {
	got := DisplayList("=?gb2312?B?1tDOxA==?= <a@x.test>, Bob <b@x.test>")
	if len(got) != 2 {
		t.Fatalf("DisplayList = %#v, want 2 recipients", got)
	}
	list, ok := ParseAddresses("=?shift_jis?B?k/qWe4zq?= <a@x.test>, Bob <b@x.test>")
	if !ok || len(list) != 2 || list[0].Email != "a@x.test" || list[1].Email != "b@x.test" {
		t.Fatalf("ParseAddresses = %#v ok=%v", list, ok)
	}
	// A subject is different: it stays visibly encoded rather than garbled.
	if d := DecodeHeader("=?gb2312?B?1tDOxA==?="); d != "=?gb2312?B?1tDOxA==?=" {
		t.Errorf("subject in an unsupported charset = %q", d)
	}
}

// A decoded display name must not be able to carry a second address: a client
// that takes the first <...> of a displayed From would reply to the wrong one.
func TestDisplayNameCannotSmuggleAnAddress(t *testing.T) {
	list, ok := ParseAddresses("=?utf-8?B?RXZlIDx2aWN0aW1AeC50ZXN0Pg==?= <attacker@evil.test>")
	if !ok || len(list) != 1 || list[0].Email != "attacker@evil.test" {
		t.Fatalf("ParseAddresses = %#v ok=%v", list, ok)
	}
	d := list[0].Display()
	if strings.Count(d, "<") != 1 || !strings.HasSuffix(d, "<attacker@evil.test>") {
		t.Errorf("Display = %q: the name carries its own <address>", d)
	}
}

// Control characters in a decoded header never reach the output.
func TestDecodedHeadersDropControlCharacters(t *testing.T) {
	for in, want := range map[string]string{
		"=?utf-8?q?a=0D=0AX-Evil:_1?=": "a  X-Evil: 1",
		"=?us-ascii?q?a=00b?=":         "a b",
		"tab\tkept =?utf-8?q?ok?=":     "tab\tkept ok",
	} {
		if got := DecodeHeader(in); got != want {
			t.Errorf("DecodeHeader(%q) = %q, want %q", in, got, want)
		}
	}
	list, _ := ParseAddresses("=?utf-8?q?Ev=0D=0Ail?= <e@x.test>")
	if len(list) != 1 || strings.ContainsAny(list[0].Name, "\r\n\x00") {
		t.Errorf("name keeps control characters: %#v", list)
	}
}
