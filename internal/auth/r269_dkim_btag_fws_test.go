package auth

import "testing"

// F7431: RFC 6376 §3.2 allows FWS around "=" in a tag-spec; the b= value must
// still be emptied when building the hash input.
func TestR269_DKIMHeaderWithoutSigTagFWS(t *testing.T) {
	for in, want := range map[string]string{
		"v=1; b=SIG":             "v=1; b=",
		"v=1; b = SIG":           "v=1; b =",
		"v=1; b\t=\tSIG; h=from": "v=1; b\t=; h=from",
		"v=1; bh = AAA; b = S":   "v=1; bh = AAA; b =",
	} {
		if got := dkimHeaderWithoutSig(in); got != want {
			t.Errorf("dkimHeaderWithoutSig(%q) = %q, want %q", in, got, want)
		}
	}
}
