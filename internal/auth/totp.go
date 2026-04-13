package auth

// TOTP 2FA is wired into the API login flow (internal/api/server.go handleLogin)
// when account.TOTPEnabled is true. GenerateTOTPUri and ValidateTOTPAt provide
// full TOTP provisioning and verification.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" // #nosec G505 -- SHA1 required by RFC 6238 for TOTP compatibility
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"hash"
	"math"
	"net/url"
	"strings"
	"time"
)

const (
	// TOTPDefaultPeriod is the default time step in seconds (30s per RFC 6238)
	TOTPDefaultPeriod = 30
	// TOTPDefaultDigits is the default number of digits (6 per RFC 6238)
	TOTPDefaultDigits = 6
	// TOTPSecretLength is the length of generated secrets in bytes
	TOTPSecretLength = 20
)

// TOTPAlgorithm represents the hash algorithm used for TOTP
type TOTPAlgorithm int

const (
	TOTPAlgorithmSHA1 TOTPAlgorithm = iota
	TOTPAlgorithmSHA256
)

// GenerateTOTPSecret generates a new random TOTP secret (base32-encoded)
func GenerateTOTPSecret() (string, error) {
	secret := make([]byte, TOTPSecretLength)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("failed to generate TOTP secret: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret), nil
}

// GenerateTOTPUri generates the otpauth:// URI for QR code provisioning
// Note: RFC 6238 specifies SHA1 as the default algorithm.
// SHA256 is a non-standard extension supported by some authenticators.
func GenerateTOTPUri(secret, account, issuer string, algo TOTPAlgorithm) string {
	v := url.Values{}
	v.Set("secret", secret)
	v.Set("issuer", issuer)
	if algo == TOTPAlgorithmSHA256 {
		v.Set("algorithm", "SHA256")
	} else {
		v.Set("algorithm", "SHA1")
	}
	v.Set("digits", fmt.Sprintf("%d", TOTPDefaultDigits))
	v.Set("period", fmt.Sprintf("%d", TOTPDefaultPeriod))
	return fmt.Sprintf("otpauth://totp/%s:%s?%s", issuer, url.PathEscape(account), v.Encode())
}

// ValidateTOTP validates a TOTP code against a secret at the current time.
// It checks the current time step and one step before/after for clock drift.
// Uses SHA1 by default (RFC 6238 compliant).
func ValidateTOTP(secret, code string) bool {
	return ValidateTOTPAt(secret, code, time.Now(), TOTPAlgorithmSHA1)
}

// ValidateTOTPAt validates a TOTP code against a secret at a specific time.
// The algorithm parameter specifies which hash to use (SHA1 or SHA256).
func ValidateTOTPAt(secret, code string, now time.Time, algo TOTPAlgorithm) bool {
	valid, _ := ValidateTOTPAtWithStep(secret, code, now, algo)
	return valid
}

// ValidateTOTPAtWithStep validates a TOTP code and returns the matched time step.
// Returns (-1, false) if no valid time step matches.
func ValidateTOTPAtWithStep(secret, code string, now time.Time, algo TOTPAlgorithm) (bool, int64) {
	if len(code) != TOTPDefaultDigits {
		return false, -1
	}

	key, err := decodeTOTPSecret(secret)
	if err != nil {
		return false, -1
	}

	unixTime := now.Unix()
	if unixTime < 0 {
		return false, -1
	}
	// #nosec G115 -- unixTime is validated non-negative above
	timeStep := uint64(unixTime) / TOTPDefaultPeriod

	// Check current, -1, and +1 time steps for clock drift tolerance
	for _, offset := range []int64{-1, 0, 1} {
		var ts uint64
		if offset < 0 {
			absOffset := uint64(-offset)
			if timeStep < absOffset {
				continue
			}
			ts = timeStep - absOffset
		} else {
			// #nosec G115 -- offset is 0 or 1, cannot overflow
			ts = timeStep + uint64(offset)
		}
		expected := computeTOTP(key, ts, TOTPDefaultDigits, algo)
		// Use constant-time comparison to prevent timing attacks
		if subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			if ts > math.MaxInt64 {
				return false, -1
			}
			// #nosec G115 -- ts is bounded above by math.MaxInt64
			return true, int64(ts)
		}
	}

	return false, -1
}

// decodeTOTPSecret decodes a base32 TOTP secret to bytes
func decodeTOTPSecret(secret string) ([]byte, error) {
	secretUpper := strings.ToUpper(strings.TrimSpace(secret))
	// Add padding if needed
	switch len(secretUpper) % 8 {
	case 2:
		secretUpper += "======"
	case 4:
		secretUpper += "===="
	case 5:
		secretUpper += "==="
	case 7:
		secretUpper += "="
	}
	return base32.StdEncoding.DecodeString(secretUpper)
}

// computeTOTP computes a TOTP code for the given key, time step, and algorithm.
func computeTOTP(key []byte, timeStep uint64, digits int, algo TOTPAlgorithm) string {
	// Encode time step as big-endian 8-byte array
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, timeStep)

	// Create hasher based on algorithm
	var h func() hash.Hash
	if algo == TOTPAlgorithmSHA256 {
		h = sha256.New
	} else {
		h = sha1.New
	}

	// HMAC with selected algorithm
	mac := hmac.New(h, key)
	mac.Write(buf)
	hash := mac.Sum(nil)

	// Dynamic truncation (RFC 4226)
	offset := hash[len(hash)-1] & 0x0f
	code := binary.BigEndian.Uint32(hash[offset:offset+4]) & 0x7fffffff

	// Modulo to get the desired number of digits
	mod := uint32(math.Pow10(digits))
	code = code % mod

	return fmt.Sprintf(fmt.Sprintf("%%0%dd", digits), code)
}
