package auth

// Regression tests for F5271 (SCRAM AuthMessage).
// RFC 7677 §3 SCRAM-SHA-256 test vector; AuthMessage per RFC 5802 §3 is
// client-first-message-bare + "," + server-first-message + "," +
// client-final-message-without-proof.

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

const (
	scramVecBare        = "n=user,r=rOprNGfwEbeRWgbNEkqO"
	scramVecServerFirst = "r=rOprNGfwEbeRWgbNEkqO%hvYDpWUa2RaTCAfuxFIlj)hNlF$k0,s=W22ZaJ0SNY7soEsUEjb6gQ==,i=4096"
	scramVecFinalNoPf   = "c=biws,r=rOprNGfwEbeRWgbNEkqO%hvYDpWUa2RaTCAfuxFIlj)hNlF$k0"
	scramVecProof       = "dHzbZapWIk4jUhN+Ute9ytag9zjfMHgsqmmiz7AndVQ="
	scramVecServerSig   = "6rriTRBi23WpRR/wtup+mMhUZUn/dB5nLTJRsjl95G4="
)

func scramVecKeys(t *testing.T) (*SCRAMSHA256, []byte) {
	t.Helper()
	salt, _ := base64.StdEncoding.DecodeString("W22ZaJ0SNY7soEsUEjb6gQ==")
	s, err := NewSCRAMSHA256("pencil", salt, 4096)
	if err != nil {
		t.Fatalf("INVALID: %v", err)
	}
	ck := hmac.New(sha256.New, s.SaltedPassword())
	ck.Write([]byte("Client Key"))
	return s, ck.Sum(nil)
}

// TestSCRAMHelpers_RFC7677Vector: before F5271 the helpers concatenated the
// AuthMessage parts without "," and could not reproduce the RFC vector.
func TestSCRAMHelpers_RFC7677Vector(t *testing.T) {
	s, clientKey := scramVecKeys(t)
	if got := base64.StdEncoding.EncodeToString(ServerSignature(s.ServerKey(), scramVecBare, scramVecServerFirst, scramVecFinalNoPf)); got != scramVecServerSig {
		t.Errorf("ServerSignature = %s, want %s", got, scramVecServerSig)
	}
	if got := base64.StdEncoding.EncodeToString(ComputeSignatureKey(s.SaltedPassword(), scramVecBare, scramVecServerFirst, scramVecFinalNoPf)); got != scramVecServerSig {
		t.Errorf("ComputeSignatureKey = %s, want %s", got, scramVecServerSig)
	}
	want, _ := base64.StdEncoding.DecodeString(scramVecProof)
	if got := XORBytes(clientKey, ClientProof(s.StoredKey(), scramVecBare, scramVecServerFirst, scramVecFinalNoPf)); !bytes.Equal(got, want) {
		t.Errorf("ClientKey XOR ClientProof() = %s, want %s", base64.StdEncoding.EncodeToString(got), scramVecProof)
	}
}

// TestSCRAMHelpers_AuthMessageSeparators pins the exact AuthMessage layout,
// including empty parts.
func TestSCRAMHelpers_AuthMessageSeparators(t *testing.T) {
	if got := scramAuthMessage("a", "b", "c"); got != "a,b,c" {
		t.Errorf("scramAuthMessage = %q, want %q", got, "a,b,c")
	}
	if got := scramAuthMessage("", "", ""); got != ",," {
		t.Errorf("scramAuthMessage empty = %q, want %q", got, ",,")
	}
	// Moving a byte across a part boundary must change the signature.
	key := []byte("k")
	if bytes.Equal(ServerSignature(key, "ab", "c", "d"), ServerSignature(key, "a", "bc", "d")) {
		t.Error("ServerSignature is ambiguous across part boundaries")
	}
}

// TestSCRAMHelpers_BuilderRoundTrip: proof built from the package builders
// verifies against the server-side helpers.
func TestSCRAMHelpers_BuilderRoundTrip(t *testing.T) {
	salt := []byte("0123456789abcdef")
	s, err := NewSCRAMSHA256("pw", salt, 4096)
	if err != nil {
		t.Fatal(err)
	}
	bare := BuildClientFirstMessage("", "bob", "", "cnonce")
	sf := BuildServerFirstMessage("cnonce"+"snonce", salt, 4096)
	finalNoProof := "c=biws,r=cnoncesnonce"
	sig1 := ServerSignature(s.ServerKey(), bare, sf, finalNoProof)
	sig2 := ComputeSignatureKey(s.SaltedPassword(), bare, sf, finalNoProof)
	if !VerifyServerSignature(sig1, sig2) {
		t.Fatal("ServerSignature and ComputeSignatureKey disagree")
	}
}
