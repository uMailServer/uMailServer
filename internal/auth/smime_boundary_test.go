package auth

import (
	"encoding/base64"
	"strings"
	"testing"
)

// TestParseMultipart_ExtractsBoundaryFromContentTypeParameter pins the
// contract that every producer/consumer pair in this package depends on:
// parseMultipart must find the MIME boundary, which per RFC 2045 appears as a
// PARAMETER of the Content-Type header:
//
//	Content-Type: multipart/encrypted; boundary=X; protocol="application/pgp-encrypted"
//
// The old code looked for a line that *starts with* "boundary=", so it never
// matched a real Content-Type line and both EncryptMessage implementations
// (openpgp.go:98 multipart/encrypted, smime.go:70 multipart/signed) produced
// messages that this function can never parse. Existing tests only fed it
// inputs that fail before the boundary lookup, so the gap was never covered.
func TestParseMultipart_ExtractsBoundaryFromContentTypeParameter(t *testing.T) {
	msg := "Content-Type: multipart/encrypted; boundary=_BOUND_; protocol=\"application/pgp-encrypted\"\r\n" +
		"\r\n" +
		"--_BOUND_\r\n" +
		"Content-Type: application/pgp-encrypted\r\n\r\n" +
		"Version: 1\r\n" +
		"--_BOUND_\r\n" +
		"Content-Type: application/octet-stream\r\n\r\n" +
		"QUJD\r\n" +
		"--_BOUND_--\r\n"

	parts, err := parseMultipart([]byte(msg))
	if err != nil {
		t.Fatalf("FAIL: parseMultipart could not extract the boundary from a standard "+
			"Content-Type parameter (smime.go boundary lookup only matches lines starting "+
			"with \"boundary=\"): %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("FAIL: got %d parts, want 2 (version part + ciphertext part)", len(parts))
	}
	if !strings.Contains(string(parts[0].Content), "Version: 1") {
		t.Errorf("parts[0] = %q, want the \"Version: 1\" part", parts[0].Content)
	}
	if _, err := base64.StdEncoding.DecodeString(string(parts[1].Content)); err != nil {
		t.Errorf("parts[1] = %q should be base64 ciphertext (%v)", parts[1].Content, err)
	}
}

// TestParseMultipart_Control_QuotedAndTrailingBoundary is the control.
// The old code did handle a line beginning with boundary="..." and a trailing
// "--boundary--"; both must keep working after the fix, so this passes before
// and after and proves the fix did not regress the shapes that already parsed.
func TestParseMultipart_Control_QuotedAndTrailingBoundary(t *testing.T) {
	msg := "boundary=\"_B_\"\r\n" +
		"\r\n" +
		"--_B_\r\n" +
		"Content-Type: text/plain\r\n\r\n" +
		"hello\r\n" +
		"--_B_--\r\n"

	parts, err := parseMultipart([]byte(msg))
	if err != nil {
		t.Fatalf("control: parseMultipart regressed on a line-initial boundary: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("control: got %d parts, want 1", len(parts))
	}
	if string(parts[0].Content) != "hello" {
		t.Errorf("control: content = %q, want %q", parts[0].Content, "hello")
	}
}
