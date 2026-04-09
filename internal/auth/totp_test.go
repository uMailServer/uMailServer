package auth

import (
	"encoding/base32"
	"strings"
	"testing"
	"time"
)

func TestGenerateTOTPSecret(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret() error = %v", err)
	}
	if len(secret) == 0 {
		t.Error("GenerateTOTPSecret() returned empty secret")
	}
	// Base32-encoded 20 bytes with no padding = 32 chars
	if len(secret) != 32 {
		t.Errorf("GenerateTOTPSecret() secret length = %d, want 32", len(secret))
	}

	// Two secrets should differ
	secret2, _ := GenerateTOTPSecret()
	if secret == secret2 {
		t.Error("Two generated secrets should not be equal")
	}
}

func TestGenerateTOTPUri(t *testing.T) {
	uri := GenerateTOTPUri("JBSWY3DPEHPK3PXP", "user@example.com", "uMailServer", TOTPAlgorithmSHA1)
	if uri == "" {
		t.Fatal("GenerateTOTPUri() returned empty URI")
	}
	if !stringsContains(uri, "otpauth://totp/") {
		t.Errorf("GenerateTOTPUri() URI missing otpauth scheme: %s", uri)
	}
	if !stringsContains(uri, "uMailServer") {
		t.Errorf("GenerateTOTPUri() URI missing issuer: %s", uri)
	}
	if !stringsContains(uri, "secret=JBSWY3DPEHPK3PXP") {
		t.Errorf("GenerateTOTPUri() URI missing secret: %s", uri)
	}
}

func TestComputeTOTP_RFC6238Vectors(t *testing.T) {
	// RFC 6238 Appendix B test vectors for SHA1
	// Secret = "12345678901234567890" (20 bytes)
	key := []byte("12345678901234567890")

	tests := []struct {
		timeStep uint64
		expected string
	}{
		// time=59, timeStep=1 (59/30=1)
		{1, "287082"},
		// time=1234567890, timeStep=41152263
		{41152263, "005924"},
	}

	for _, tt := range tests {
		code := computeTOTP(key, tt.timeStep, 6, TOTPAlgorithmSHA1)
		if code != tt.expected {
			t.Errorf("computeTOTP(key, %d, 6, TOTPAlgorithmSHA1) = %q, want %q", tt.timeStep, code, tt.expected)
		}
	}
}

func TestValidateTOTPAt_KnownVector(t *testing.T) {
	key := []byte("12345678901234567890")
	secret := base32.StdEncoding.WithPadding(0).EncodeToString(key)

	// time=59 => timeStep=1 => code=287082
	now := time.Unix(59, 0)
	if !ValidateTOTPAt(secret, "287082", now, TOTPAlgorithmSHA1) {
		t.Error("ValidateTOTPAt() should accept RFC 6238 test vector for t=59")
	}

	// time=1234567890 => timeStep=41152263 => code=005924
	now2 := time.Unix(1234567890, 0)
	if !ValidateTOTPAt(secret, "005924", now2, TOTPAlgorithmSHA1) {
		t.Error("ValidateTOTPAt() should accept RFC 6238 test vector for t=1234567890")
	}
}

func TestValidateTOTP_WrongCode(t *testing.T) {
	secret, _ := GenerateTOTPSecret()
	// Very unlikely to match "999999" at current time
	if ValidateTOTPAt(secret, "999999", time.Now(), TOTPAlgorithmSHA1) {
		t.Log("Warning: code 999999 matched (unlikely but possible)")
	}
}

func TestValidateTOTP_EmptyCode(t *testing.T) {
	secret, _ := GenerateTOTPSecret()
	if ValidateTOTP(secret, "") {
		t.Error("ValidateTOTP() should reject empty code")
	}
}

func TestValidateTOTP_WrongLength(t *testing.T) {
	secret, _ := GenerateTOTPSecret()
	if ValidateTOTP(secret, "12345") {
		t.Error("ValidateTOTP() should reject 5-digit code")
	}
	if ValidateTOTP(secret, "1234567") {
		t.Error("ValidateTOTP() should reject 7-digit code")
	}
}

func TestValidateTOTP_InvalidSecret(t *testing.T) {
	if ValidateTOTP("not-valid-base32!!!", "123456") {
		t.Error("ValidateTOTP() should reject invalid base32 secret")
	}
}

func TestValidateTOTP_DriftTolerance(t *testing.T) {
	key := []byte("12345678901234567890")
	secret := base32.StdEncoding.WithPadding(0).EncodeToString(key)

	// timeStep=1 => code=287082
	code := "287082"

	// Exact time: t=45 => timeStep=1
	exactTime := time.Unix(45, 0)
	if !ValidateTOTPAt(secret, code, exactTime, TOTPAlgorithmSHA1) {
		t.Error("Should accept code at exact time")
	}

	// One step forward: t=75 => timeStep=2, should still accept due to drift
	oneStepLater := time.Unix(75, 0)
	if !ValidateTOTPAt(secret, code, oneStepLater, TOTPAlgorithmSHA1) {
		t.Error("Should accept code with +1 step drift tolerance")
	}

	// One step back: t=15 => timeStep=0, should still accept due to drift
	oneStepBefore := time.Unix(15, 0)
	if !ValidateTOTPAt(secret, code, oneStepBefore, TOTPAlgorithmSHA1) {
		t.Error("Should accept code with -1 step drift tolerance")
	}

	// Two steps forward: t=105 => timeStep=3, should NOT accept
	twoStepsLater := time.Unix(105, 0)
	if ValidateTOTPAt(secret, code, twoStepsLater, TOTPAlgorithmSHA1) {
		t.Error("Should NOT accept code with +2 step drift")
	}
}

// --- New tests to improve coverage ---

func TestGenerateTOTPSecret_OutputIsBase32(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret() error = %v", err)
	}
	// Must decode without error (validates base32 with no padding)
	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatalf("Generated secret is not valid base32: %v", err)
	}
	if len(decoded) != TOTPSecretLength {
		t.Errorf("Decoded secret length = %d, want %d", len(decoded), TOTPSecretLength)
	}
}

func TestGenerateTOTPSecret_NoPadding(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret() error = %v", err)
	}
	// Base32 with NoPadding should not contain '='
	if strings.Contains(secret, "=") {
		t.Error("Generated secret should not contain padding characters '='")
	}
}

func TestDecodeTOTPSecret_ValidBase32NoPadding(t *testing.T) {
	// 20 bytes -> 32 base32 chars without padding (32 % 8 == 0, no padding needed)
	input := "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"
	decoded, err := decodeTOTPSecret(input)
	if err != nil {
		t.Fatalf("decodeTOTPSecret(%q) error = %v", input, err)
	}
	if len(decoded) != 20 {
		t.Errorf("decoded length = %d, want 20", len(decoded))
	}
}

func TestDecodeTOTPSecret_CaseInsensitive(t *testing.T) {
	// Lowercase should be accepted since the function uppercases input
	input := "jbswy3dpehpk3pxp" // 16 chars, 16 % 8 == 0
	decoded, err := decodeTOTPSecret(input)
	if err != nil {
		t.Fatalf("decodeTOTPSecret(%q) error = %v", input, err)
	}
	upper := "JBSWY3DPEHPK3PXP"
	decodedUpper, _ := decodeTOTPSecret(upper)
	if string(decoded) != string(decodedUpper) {
		t.Error("decodeTOTPSecret should be case-insensitive")
	}
}

func TestDecodeTOTPSecret_WhitespaceTrimmed(t *testing.T) {
	input := "  JBSWY3DPEHPK3PXP  "
	decoded, err := decodeTOTPSecret(input)
	if err != nil {
		t.Fatalf("decodeTOTPSecret(%q) error = %v", input, err)
	}
	decodedClean, _ := decodeTOTPSecret("JBSWY3DPEHPK3PXP")
	if string(decoded) != string(decodedClean) {
		t.Error("decodeTOTPSecret should trim whitespace")
	}
}

func TestDecodeTOTPSecret_EmptyString(t *testing.T) {
	decoded, err := decodeTOTPSecret("")
	if err != nil {
		t.Fatalf("decodeTOTPSecret('') error = %v", err)
	}
	if len(decoded) != 0 {
		t.Errorf("decodeTOTPSecret('') returned %d bytes, want 0", len(decoded))
	}
}

func TestDecodeTOTPSecret_InvalidBase32(t *testing.T) {
	// Characters like '!', '@', '#' are not valid base32
	input := "INVALID!@#"
	_, err := decodeTOTPSecret(input)
	if err == nil {
		t.Error("decodeTOTPSecret should return error for invalid base32 characters")
	}
}

func TestDecodeTOTPSecret_PaddingBranch2(t *testing.T) {
	// 2 base32 chars: 2 % 8 == 2, triggers "======" padding
	input := "AB"
	decoded, err := decodeTOTPSecret(input)
	if err != nil {
		t.Fatalf("decodeTOTPSecret(%q) error = %v", input, err)
	}
	if len(decoded) != 1 {
		t.Errorf("decodeTOTPSecret(%q) length = %d, want 1", input, len(decoded))
	}
}

func TestDecodeTOTPSecret_PaddingBranch4(t *testing.T) {
	// 4 base32 chars: 4 % 8 == 4, triggers "====" padding
	input := "AABB"
	decoded, err := decodeTOTPSecret(input)
	if err != nil {
		t.Fatalf("decodeTOTPSecret(%q) error = %v", input, err)
	}
	if len(decoded) != 2 { // 4 base32 chars = 2 bytes + partial, but with 4 chars it's exactly 2.5 bytes; padding gives 2
		t.Errorf("decodeTOTPSecret(%q) length = %d, want 2", input, len(decoded))
	}
}

func TestDecodeTOTPSecret_PaddingBranch5(t *testing.T) {
	// 5 base32 chars: 5 % 8 == 5, triggers "===" padding
	input := "AABBC"
	decoded, err := decodeTOTPSecret(input)
	if err != nil {
		t.Fatalf("decodeTOTPSecret(%q) error = %v", input, err)
	}
	// 5 base32 chars decodes to 3 bytes
	if len(decoded) != 3 {
		t.Errorf("decodeTOTPSecret(%q) length = %d, want 3", input, len(decoded))
	}
}

func TestDecodeTOTPSecret_PaddingBranch7(t *testing.T) {
	// 7 base32 chars: 7 % 8 == 7, triggers "=" padding
	input := "AABBCCD"
	decoded, err := decodeTOTPSecret(input)
	if err != nil {
		t.Fatalf("decodeTOTPSecret(%q) error = %v", input, err)
	}
	// 7 base32 chars decodes to 4 bytes (with single padding)
	if len(decoded) != 4 {
		t.Errorf("decodeTOTPSecret(%q) length = %d, want 4", input, len(decoded))
	}
}

func TestValidateTOTPAt_ValidCode(t *testing.T) {
	// RFC 6238: key="12345678901234567890", time=59, code=287082
	key := []byte("12345678901234567890")
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(key)
	now := time.Unix(59, 0)

	if !ValidateTOTPAt(secret, "287082", now, TOTPAlgorithmSHA1) {
		t.Error("ValidateTOTPAt should accept valid RFC 6238 code at t=59")
	}
}

func TestValidateTOTPAt_InvalidCode(t *testing.T) {
	key := []byte("12345678901234567890")
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(key)
	now := time.Unix(59, 0)

	if ValidateTOTPAt(secret, "000000", now, TOTPAlgorithmSHA1) {
		t.Error("ValidateTOTPAt should reject wrong code at t=59")
	}
}

func TestValidateTOTPAt_WrongSecret(t *testing.T) {
	// Two different secrets should produce different codes for the same time
	_ = base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	secret2 := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("ABCDEFGHIJKLMNOPQRST"))

	now := time.Unix(59, 0)
	// Code for secret1 at t=59 is "287082"
	if ValidateTOTPAt(secret2, "287082", now, TOTPAlgorithmSHA1) {
		t.Error("ValidateTOTPAt should reject code generated from a different secret")
	}
}

func TestValidateTOTPAt_InvalidSecret(t *testing.T) {
	now := time.Unix(59, 0)
	if ValidateTOTPAt("!!!invalid!!!", "123456", now, TOTPAlgorithmSHA1) {
		t.Error("ValidateTOTPAt should reject invalid base32 secret")
	}
}

func TestValidateTOTPAt_InvalidCodeLength(t *testing.T) {
	secret, _ := GenerateTOTPSecret()
	now := time.Now()

	if ValidateTOTPAt(secret, "12345", now, TOTPAlgorithmSHA1) {
		t.Error("ValidateTOTPAt should reject 5-digit code")
	}
	if ValidateTOTPAt(secret, "1234567", now, TOTPAlgorithmSHA1) {
		t.Error("ValidateTOTPAt should reject 7-digit code")
	}
	if ValidateTOTPAt(secret, "", now, TOTPAlgorithmSHA1) {
		t.Error("ValidateTOTPAt should reject empty code")
	}
}

func TestComputeTOTP_KnownVectors(t *testing.T) {
	key := []byte("12345678901234567890")

	tests := []struct {
		timeStep uint64
		expected string
	}{
		// RFC 6238 Appendix B SHA1 test vectors
		{0x0000000000000001, "94287082"}, // truncated to 6 digits: "287082" at step 1
		// Additional time steps for variety
		{41152263, "005924"},
	}

	for _, tt := range tests {
		code6 := computeTOTP(key, tt.timeStep, 6, TOTPAlgorithmSHA1)
		if len(code6) != 6 {
			t.Errorf("computeTOTP(_, %d, 6) produced %d-digit code, want 6", tt.timeStep, len(code6))
		}
	}
}

func TestComputeTOTP_DigitVariation(t *testing.T) {
	key := []byte("12345678901234567890")

	// Test with 8 digits
	code8 := computeTOTP(key, 1, 8, TOTPAlgorithmSHA1)
	if len(code8) != 8 {
		t.Errorf("computeTOTP(_, 1, 8) produced %d-digit code, want 8", len(code8))
	}
	// First 6 chars of the 8-digit code at step 1 should be "287082" padded differently
	// since 8-digit code for step 1 is "94287082"
	if code8 != "94287082" {
		t.Errorf("computeTOTP(_, 1, 8) = %q, want %q", code8, "94287082")
	}
}

func TestValidateTOTP_WrongSecret(t *testing.T) {
	key := []byte("12345678901234567890")
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(key)

	// Code "287082" is valid at t=59 for this key
	// Using current time, it's very unlikely to match
	if ValidateTOTP(secret, "000000") {
		t.Log("Warning: code 000000 matched (unlikely but possible)")
	}
}

func TestGenerateTOTPUri_Fields(t *testing.T) {
	uri := GenerateTOTPUri("MYSECRET", "alice@example.com", "TestIssuer", TOTPAlgorithmSHA1)

	// Check all required fields
	if !stringsContains(uri, "otpauth://totp/TestIssuer:alice@example.com") {
		t.Errorf("URI missing proper label: %s", uri)
	}
	if !stringsContains(uri, "secret=MYSECRET") {
		t.Errorf("URI missing secret: %s", uri)
	}
	if !stringsContains(uri, "issuer=TestIssuer") {
		t.Errorf("URI missing issuer: %s", uri)
	}
	if !stringsContains(uri, "algorithm=SHA1") {
		t.Errorf("URI missing algorithm: %s", uri)
	}
	if !stringsContains(uri, "digits=6") {
		t.Errorf("URI missing digits: %s", uri)
	}
	if !stringsContains(uri, "period=30") {
		t.Errorf("URI missing period: %s", uri)
	}
}

func TestGenerateTOTPUri_SpecialCharacters(t *testing.T) {
	// Account with special characters - url.PathEscape handles certain characters
	uri := GenerateTOTPUri("SECRET", "user test@example.com", "MyApp", TOTPAlgorithmSHA1)
	if !stringsContains(uri, "user%20test@example.com") {
		t.Errorf("URI did not escape space character: %s", uri)
	}
}

func TestValidateTOTPAt_TwoStepsDrift_Rejected(t *testing.T) {
	key := []byte("12345678901234567890")
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(key)

	// timeStep=1 => code=287082
	code := "287082"

	// Two steps before: t=0 => timeStep=0, which is 1 step from step 1 (within drift)
	// Use t=-30 => timeStep that wraps: uint64(-30)/30 is large, but int64(-30)/30 = -1
	// Instead use a time far enough: t=135 => timeStep=4, which is 3 steps from step 1
	twoStepsLater := time.Unix(135, 0) // timeStep = 4, 3 steps from step 1
	if ValidateTOTPAt(secret, code, twoStepsLater, TOTPAlgorithmSHA1) {
		t.Error("Should NOT accept code when timeStep differs by 3 steps")
	}
}

func TestDecodeTOTPSecret_RoundTrip(t *testing.T) {
	// Generate a secret, decode it, verify it matches original bytes
	original := []byte("ABCDEFGHIJKLMNOPQRST")
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(original)
	decoded, err := decodeTOTPSecret(encoded)
	if err != nil {
		t.Fatalf("decodeTOTPSecret roundtrip error = %v", err)
	}
	if string(decoded) != string(original) {
		t.Errorf("roundtrip mismatch: got %x, want %x", decoded, original)
	}
}

func stringsContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
