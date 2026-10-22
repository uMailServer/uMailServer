package auth

import (
	"strings"
	"testing"
)

// RFC 5802 §5.1: client-first-message = gs2-header client-first-message-bare,
// with gs2-header = gs2-cbind-flag "," [ "a=" authzid ] ",". The c= attribute
// belongs to the client-FINAL message only. BuildClientFirstMessage emitted
// no GS2 header at all and put a c= attribute into the client-first message,
// so its output was not a conformant client-first message (this server's own
// session handler rejects it: the GS2 SplitN requires three parts).

func TestBuildClientFirstMessageCarriesGS2Header(t *testing.T) {
	tests := []struct {
		name        string
		authzID     string
		channelBind string
		want        string
	}{
		{"no binding, no authzid", "", "", "n,,n=bob,r=cnonce"},
		{"no binding, authzid", "alice", "", "n,a=alice,n=bob,r=cnonce"},
		{"channel binding", "", "tls-unique", "p=tls-unique,,n=bob,r=cnonce"},
		{"channel binding, authzid", "alice", "tls-unique", "p=tls-unique,a=alice,n=bob,r=cnonce"},
	}
	for _, tc := range tests {
		got := BuildClientFirstMessage(tc.authzID, "bob", tc.channelBind, "cnonce")
		if got != tc.want {
			t.Errorf("FAIL: %s = %q, want conformant GS2 form %q (RFC 5802 §5.1)", tc.name, got, tc.want)
			continue
		}
		// The c= attribute must never appear in the client-first message.
		if strings.Contains(got, "c=") {
			t.Errorf("FAIL: %s contains c= in the client-first message (c= belongs to client-final only)", tc.name)
		}
	}
}
