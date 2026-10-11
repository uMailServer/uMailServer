package search

import (
	"strings"
	"testing"
)

func TestR208ExtractMIME(t *testing.T) {
	cases := []struct{ name, msg, want, notWant string }{
		{"plain-lt", "Subject: x\r\n\r\nif a < b then zebra\r\n", "zebra", ""},
		{"base64", "Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\nxLFzcGFydGEgemVicmE=\r\n", "zebra", "xLFz"},
		{"qp-latin1", "Content-Type: text/plain; charset=iso-8859-1\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\ncaf=E9 zebra\r\n", "café", ""},
		{"html-style", "Content-Type: text/html\r\n\r\n<style>.x{color:red}</style><p>Tom &amp; zebra</p>", "Tom & zebra", "color"},
		{"multipart", "Content-Type: multipart/mixed; boundary=B\r\n\r\n--B\r\nContent-Type: text/plain\r\n\r\nhello zebra\r\n--B\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment\r\nContent-Transfer-Encoding: base64\r\n\r\nQUJD\r\n--B--\r\n", "hello zebra", "QUJD"},
	}
	for _, c := range cases {
		got := extractTextContent([]byte(c.msg))
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: got %q want contains %q", c.name, got, c.want)
		}
		if c.notWant != "" && strings.Contains(got, c.notWant) {
			t.Errorf("%s: got %q must not contain %q", c.name, got, c.notWant)
		}
	}
	if d := decodeHeaderValue("=?UTF-8?B?w5xtbMOkdXQ=?="); d != "Ümläut" {
		t.Errorf("rfc2047: %q", d)
	}
}
