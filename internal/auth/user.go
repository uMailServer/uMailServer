package auth

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

var (
	// ErrInvalidCredentials is returned when authentication fails
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrUserNotFound is returned when a user doesn't exist
	ErrUserNotFound = errors.New("user not found")
	// ErrUserDisabled is returned when a user account is disabled
	ErrUserDisabled = errors.New("user account is disabled")
)

// UserAuthenticator handles user authentication
type UserAuthenticator struct {
	// GetUser is a function that retrieves user data by email
	GetUser func(email string) (*UserData, error)
}

// UserData holds user authentication data
type UserData struct {
	Email        string
	PasswordHash string
	Domain       string
	IsActive     bool
	TOTPSecret   string // For 2FA
}

// NewUserAuthenticator creates a new authenticator
func NewUserAuthenticator(getUser func(email string) (*UserData, error)) *UserAuthenticator {
	return &UserAuthenticator{
		GetUser: getUser,
	}
}

// ErrTOTPRequired indicates password is correct but TOTP code is needed
var ErrTOTPRequired = errors.New("TOTP code required")

// Authenticate validates username and password.
// If the user has TOTP enabled, returns ErrTOTPRequired after password validation.
// Use AuthenticateWithTOTP for the full 2FA flow.
func (a *UserAuthenticator) Authenticate(username, password string) (*UserData, error) {
	// Normalize username (lowercase)
	username = strings.ToLower(strings.TrimSpace(username))

	user, err := a.GetUser(username)
	if err != nil {
		return nil, ErrUserNotFound
	}

	if !user.IsActive {
		return nil, ErrUserDisabled
	}

	// Verify password
	if !VerifyPassword(password, user.PasswordHash) {
		return nil, ErrInvalidCredentials
	}

	// If user has TOTP configured, password is correct but full auth requires TOTP
	if user.TOTPSecret != "" {
		return user, ErrTOTPRequired
	}

	return user, nil
}

// AuthenticateWithTOTP performs full authentication including optional TOTP verification.
// If the user has TOTP enabled, the totpCode parameter must contain a valid TOTP code.
func (a *UserAuthenticator) AuthenticateWithTOTP(username, password, totpCode string) (*UserData, error) {
	user, err := a.Authenticate(username, password)
	if err == nil {
		// No TOTP required
		return user, nil
	}
	if err != ErrTOTPRequired {
		return nil, err
	}

	// TOTP required
	if totpCode == "" {
		return nil, ErrTOTPRequired
	}

	if !ValidateTOTP(user.TOTPSecret, totpCode) {
		return nil, ErrInvalidCredentials
	}

	return user, nil
}

// HashPassword creates a password hash using Argon2id
func HashPassword(password string) (string, error) {
	// Generate a random salt
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate salt: %w", err)
	}

	// Argon2id parameters
	// time=1, memory=64MB, threads=4
	hash := argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, 32)

	// Encode as base64 string: $argon2id$v=19$m=65536,t=1,p=4$<salt>$<hash>
	encodedHash := fmt.Sprintf("$argon2id$v=19$m=65536,t=1,p=4$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash))

	return encodedHash, nil
}

// VerifyPassword checks if a password matches a hash
func VerifyPassword(password, encodedHash string) bool {
	// Check hash format
	if !strings.HasPrefix(encodedHash, "$argon2id$") {
		// Try bcrypt for backward compatibility
		return verifyBcrypt(password, encodedHash)
	}

	// Parse Argon2id hash
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 {
		return false
	}

	// Parse parameters
	// parts[2] = v=19
	// parts[3] = m=65536,t=1,p=4
	params := parts[3]
	var memory, time uint32
	var threads uint8
	fmt.Sscanf(params, "m=%d,t=%d,p=%d", &memory, &time, &threads)

	// Decode salt and hash
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}

	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}

	// Compute hash
	computedHash := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(hash)))

	// Compare hashes using constant time comparison
	return subtle.ConstantTimeCompare(hash, computedHash) == 1
}

// verifyBcrypt verifies a bcrypt hash (for backward compatibility)
func verifyBcrypt(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// HashPasswordBcrypt creates a password hash using bcrypt (legacy)
func HashPasswordBcrypt(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

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

// GenerateAppPassword generates a random app password
func GenerateAppPassword() (string, error) {
	// Generate 16 random bytes = 32 hex characters
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	// Format: xxxx-xxxx-xxxx-xxxx (app password format)
	return fmt.Sprintf("%x-%x-%x-%x",
		bytes[0:4],
		bytes[4:8],
		bytes[8:12],
		bytes[12:16]), nil
}
