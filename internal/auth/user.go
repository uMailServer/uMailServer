package auth

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// VerifyCRAMMD5 verifies a CRAM-MD5 challenge response
// challenge is the base64-encoded challenge sent to client
// response is the base64-encoded response from client (format: username <hex_hmac>)
// getSecret is a function that returns the user's shared secret
func VerifyCRAMMD5(challenge, response string, getSecret func(username string) (string, error)) (string, bool) {
	// Decode response
	responseBytes, err := base64.StdEncoding.DecodeString(response)
	if err != nil {
		return "", false
	}

	responseStr := string(responseBytes)
	parts := strings.SplitN(responseStr, " ", 2)
	if len(parts) != 2 {
		return "", false
	}

	username := parts[0]
	hexHMAC := parts[1]

	// Get user's shared secret
	secret, err := getSecret(username)
	if err != nil {
		return username, false
	}

	// Calculate expected HMAC
	challengeBytes, _ := base64.StdEncoding.DecodeString(challenge)
	expectedHMAC := hmac.New(md5.New, []byte(secret))
	expectedHMAC.Write(challengeBytes)
	expectedHex := hex.EncodeToString(expectedHMAC.Sum(nil))

	// Compare using constant time
	match := subtle.ConstantTimeCompare([]byte(hexHMAC), []byte(expectedHex)) == 1

	return username, match
}

// GenerateCRAMMD5Challenge generates a CRAM-MD5 challenge
func GenerateCRAMMD5Challenge() (string, string, error) {
	challenge := make([]byte, 32)
	if _, err := rand.Read(challenge); err != nil {
		return "", "", err
	}

	// Create challenge string: <random@hostname>
	challengeStr := fmt.Sprintf("<%x@umailserver>", challenge)
	challengeB64 := base64.StdEncoding.EncodeToString([]byte(challengeStr))

	return challengeStr, challengeB64, nil
}
