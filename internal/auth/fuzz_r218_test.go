package auth

import "testing"

// F6950: DKIM-Signature header and key-record parsers must never panic.
func FuzzR218DKIMParsers(f *testing.F) {
	for _, s := range []string{
		"v=1; a=rsa-sha256; d=example.com; s=sel; c=relaxed/relaxed; q=dns/txt; h=from:to:subject; bh=abc=; b=def=; t=1; x=2; l=5",
		"v=1; a=ed25519-sha256; d=e.com; s=s; h=from; bh=; b=",
		"v=1;;;=;a", "v=DKIM1; k=rsa; p=", "v=DKIM1; k=ed25519; p=AAAA", "", ";", "a=b=c; a=d", "l=99999999999999999999; t=-1",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		_, _ = parseDKIMSignature(in)
		_, _ = parseTagValueListDup(in)
		_ = parseHeaderList(in)
		_ = parseCopiedHeaders(in)
		_, _, _ = parseDKIMPublicKey(in)
	})
}
