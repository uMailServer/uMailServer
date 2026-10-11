package spam

import (
	"strings"
	"testing"
)

func has(f []string, w string) bool {
	for _, x := range f {
		if x == w {
			return true
		}
	}
	return false
}

// F6923: special features must be found with lowercase header keys.
func TestAddSpecialFeatures_CaseInsensitiveHeaders_F6923(t *testing.T) {
	fe := NewFeatureExtractor()
	f := fe.addSpecialFeatures(map[string][]string{
		"subject":      {"WIN $$ now!!!"},
		"content-type": {"text/html; charset=utf-8"},
	}, []byte("Click here"))
	for _, w := range []string{"excessive_exclamation", "money_symbol_subject", "html_only_email", "suspicious_phrase_click_here"} {
		if !has(f, w) {
			t.Errorf("missing %s in %v", w, f)
		}
	}
}

// Hostile-input table: no panics, bounded output.
func TestExtract_HostileInputsNoPanic(t *testing.T) {
	fe := NewFeatureExtractor()
	long := strings.Repeat("a", 100000)
	cases := []map[string][]string{
		nil, {}, {"": {""}}, {"From": {"<<<@>>>"}}, {"From": {"@"}}, {"To": {"a@b@c"}},
		{"From": {long + "@" + long}}, {"Subject": nil}, {"Content-Type": {";;;="}},
		{"Content-Type": {"text/html; boundary=\x00"}}, {"X": {"\xff\xfe\x00"}},
		{"Cc": {`"unterminated <a@b`}}, {"Subject": {strings.Repeat("!", 1000)}},
	}
	for i, h := range cases {
		for _, body := range [][]byte{nil, {0xff, 0xfe}, []byte(long)} {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("case %d panic: %v", i, r)
					}
				}()
				_ = fe.Extract(h, body)
			}()
		}
	}
}
