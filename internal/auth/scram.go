package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// SCRAM-SHA-256 mechanism (RFC 7677)

// SCRAMClientInitialMessage contains the parsed client initial message
type SCRAMClientInitialMessage struct {
	AuthzID     string // Authorization identity
	AuthCID     string // Authentication identity (username)
	ChannelBind string // Channel binding flag (e.g., "p=tls-unique")
	Nonce       string // Client nonce
}

// SCRAMServerFirstMessage contains the parsed server first message
type SCRAMServerFirstMessage struct {
	Salt         []byte // Base64-decoded salt
	Nonce        string // Server nonce (client + server nonce)
	Iterations   int    // Number of iterations
	SaltedPassword []byte // SaltedPassword = Hi(Normalize(password), salt, i)
}

// SCRAMClientFinalMessage contains the client final message
type SCRAMClientFinalMessage struct {
	ChannelBind  string // Channel binding data
	Nonce         string // Nonce
	ClientProof  []byte // HMAC-based proof
}

// SCRAMSHA256 implements SCRAM-SHA-256 mechanism (RFC 7677)
type SCRAMSHA256 struct {
	salt       []byte
	saltedPassword []byte
	storedKey  []byte
	serverKey  []byte
}

// NewSCRAMSHA256 creates a new SCRAM-SHA-256 authenticator with the password
func NewSCRAMSHA256(password string, salt []byte, iterations int) (*SCRAMSHA256, error) {
	// Normalize password (RFC 7616 UsernameCaseMapped - lowercase)
	normalized := strings.ToLower(password)

	// SaltedPassword = Hi(Normalized(password), salt, i)
	saltedPassword := Hi(normalized, salt, iterations)

	// StoredKey = HMAC(SaltedPassword, "Client Key")
	clientKey := hmac.New(sha256.New, saltedPassword)
	clientKey.Write([]byte("Client Key"))
	storedKey := clientKey.Sum(nil)

	// ServerKey = HMAC(SaltedPassword, "Server Key")
	serverKey := hmac.New(sha256.New, saltedPassword)
	serverKey.Write([]byte("Server Key"))

	return &SCRAMSHA256{
		salt:       salt,
		saltedPassword: saltedPassword,
		storedKey:  storedKey,
		serverKey:  serverKey.Sum(nil),
	}, nil
}

// StoredKey returns the stored key
func (s *SCRAMSHA256) StoredKey() []byte { return s.storedKey }

// ServerKey returns the server key
func (s *SCRAMSHA256) ServerKey() []byte { return s.serverKey }

// SaltedPassword returns the salted password
func (s *SCRAMSHA256) SaltedPassword() []byte { return s.saltedPassword }

// GenerateSalt generates a random salt for SCRAM
func GenerateSalt() ([]byte, error) {
	salt := make([]byte, 16)
	_, err := rand.Read(salt)
	if err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}
	return salt, nil
}

// GenerateNonce generates a random nonce for SCRAM
func GenerateNonce() (string, error) {
	b := make([]byte, 18)
	_, err := rand.Read(b)
	if err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// Hi implements PBKDF2-like function for SCRAM (RFC 7677)
func Hi(password string, salt []byte, iterations int) []byte {
	// Hi(password, salt, i) = HMAC(password, salt || INT(1)) ...
	ui := hmac.New(sha256.New, []byte(password))
	ui.Write(salt)
	ui.Write([]byte{0, 0, 0, 1}) // INT(1) in big-endian
	result := ui.Sum(nil)

	for i := 2; i <= iterations; i++ {
		ui = hmac.New(sha256.New, []byte(password))
		ui.Write(result)
		result = ui.Sum(nil)
	}
	return result
}

// BuildServerFirstMessage builds the server-first message (RFC 7677)
func BuildServerFirstMessage(nonce string, salt []byte, iterations int) string {
	// Format: r=<client-nonce><server-nonce>,s=<base64-salt>,i=<iterations>
	saltB64 := base64.StdEncoding.EncodeToString(salt)
	return fmt.Sprintf("r=%s,s=%s,i=%d", nonce, saltB64, iterations)
}

// ParseServerFirstMessage parses the server-first message (RFC 7677)
func ParseServerFirstMessage(msg string) (*SCRAMServerFirstMessage, error) {
	parts := make(map[string]string)
	for _, pair := range strings.Split(msg, ",") {
		if len(pair) < 2 {
			continue
		}
		key := pair[:1]
		val := pair[2:] // after "="
		parts[key] = val
	}

	r, ok := parts["r"]
	if !ok {
		return nil, fmt.Errorf("missing nonce in server-first message")
	}

	s, ok := parts["s"]
	if !ok {
		return nil, fmt.Errorf("missing salt in server-first message")
	}
	salt, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid salt encoding: %w", err)
	}

	iStr, ok := parts["i"]
	if !ok {
		return nil, fmt.Errorf("missing iterations in server-first message")
	}
	iterations, err := strconv.Atoi(iStr)
	if err != nil || iterations < 1 {
		return nil, fmt.Errorf("invalid iterations: %s", iStr)
	}

	return &SCRAMServerFirstMessage{
		Nonce:      r,
		Salt:       salt,
		Iterations: iterations,
	}, nil
}

// BuildClientFinalMessage builds the client-final message (RFC 7677)
func BuildClientFinalMessage(channelBind string, nonce string, clientProof []byte) string {
	// Format: c=<channel-bind>,r=<nonce>,p=<base64-client-proof>
	cB64 := base64.StdEncoding.EncodeToString([]byte(channelBind))
	pB64 := base64.StdEncoding.EncodeToString(clientProof)
	return fmt.Sprintf("c=%s,r=%s,p=%s", cB64, nonce, pB64)
}

// ParseClientFinalMessage parses the client-final message (RFC 7677)
func ParseClientFinalMessage(msg string) (*SCRAMClientFinalMessage, error) {
	parts := make(map[string]string)
	for _, pair := range strings.Split(msg, ",") {
		if len(pair) < 2 {
			continue
		}
		key := pair[:1]
		val := pair[2:]
		parts[key] = val
	}

	c, ok := parts["c"]
	if !ok {
		return nil, fmt.Errorf("missing channel binding in client-final message")
	}

	r, ok := parts["r"]
	if !ok {
		return nil, fmt.Errorf("missing nonce in client-final message")
	}

	p, ok := parts["p"]
	if !ok {
		return nil, fmt.Errorf("missing proof in client-final message")
	}

	proof, err := base64.StdEncoding.DecodeString(p)
	if err != nil {
		return nil, fmt.Errorf("invalid proof encoding: %w", err)
	}

	return &SCRAMClientFinalMessage{
		ChannelBind: c,
		Nonce:       r,
		ClientProof: proof,
	}, nil
}

// BuildServerFinalMessage builds the server-final message (RFC 7677)
func BuildServerFinalMessage(serverSignature []byte) string {
	// Format: v=<base64-server-signature>
	vB64 := base64.StdEncoding.EncodeToString(serverSignature)
	return fmt.Sprintf("v=%s", vB64)
}

// ServerSignature computes the server signature (RFC 7677)
func ServerSignature(serverKey []byte, clientFirstMessageBare string, serverFirstMessage string, clientFinalMessage string) []byte {
	// ServerSignature = HMAC(ServerKey, ClientMessage || ServerMessage || ClientFinalMessage)
	h := hmac.New(sha256.New, serverKey)
	h.Write([]byte(clientFirstMessageBare))
	h.Write([]byte(serverFirstMessage))
	h.Write([]byte(clientFinalMessage))
	return h.Sum(nil)
}

// ClientProof computes the client proof (RFC 7677)
func ClientProof(storedKey []byte, clientFirstMessageBare string, serverFirstMessage string, clientFinalMessage string) []byte {
	// ClientProof = ClientSignature = HMAC(StoredKey, AuthMessage)
	h := hmac.New(sha256.New, storedKey)
	h.Write([]byte(clientFirstMessageBare))
	h.Write([]byte(serverFirstMessage))
	h.Write([]byte(clientFinalMessage))
	return h.Sum(nil)
}

// BuildClientFirstMessage builds the client-first message (RFC 7677)
func BuildClientFirstMessage(authzID, username, channelBind, nonce string) string {
	// Format: n=,a=<authzid>,n=<username>,r=<nonce>  (no channel binding)
	// or: c=<channel-bind>,r=<nonce>,n=<username> (with channel binding)
	if channelBind != "" {
		cB64 := base64.StdEncoding.EncodeToString([]byte(channelBind))
		if authzID != "" {
			return fmt.Sprintf("a=%s,n=%s,r=%s", authzID, username, nonce)
		}
		return fmt.Sprintf("c=%s,r=%s,n=%s", cB64, nonce, username)
	}
	// Without channel binding
	if authzID != "" {
		return fmt.Sprintf("a=%s,n=%s,r=%s", authzID, username, nonce)
	}
	return fmt.Sprintf("n=%s,r=%s", username, nonce)
}

// ParseClientFirstMessage parses the client-first message (RFC 7677)
func ParseClientFirstMessage(msg string) (*SCRAMClientInitialMessage, error) {
	// Remove leading channel binding prefix if present
	msg = strings.TrimPrefix(msg, "n=")
	msg = strings.TrimPrefix(msg, "y=")
	msg = strings.TrimPrefix(msg, "p=")

	parts := make(map[string]string)
	for _, pair := range strings.Split(msg, ",") {
		if len(pair) < 2 {
			continue
		}
		key := pair[:1]
		val := pair[2:]
		parts[key] = val
	}

	var authzID, authCID, channelBind, nonce string

	if a, ok := parts["a"]; ok {
		authzID = a
	}
	if n, ok := parts["n"]; ok {
		authCID = n
	}
	if r, ok := parts["r"]; ok {
		nonce = r
	}
	if c, ok := parts["c"]; ok {
		channelBind = c
	}

	return &SCRAMClientInitialMessage{
		AuthzID:     authzID,
		AuthCID:     authCID,
		ChannelBind: channelBind,
		Nonce:       nonce,
	}, nil
}

// VerifyServerSignature verifies the server signature in the server-final message
func VerifyServerSignature(serverSig []byte, expectedServerSig []byte) bool {
	return hmac.Equal(serverSig, expectedServerSig)
}

// XORBytes performs XOR of two byte slices and returns the result
func XORBytes(a, b []byte) []byte {
	result := make([]byte, len(a))
	for i := 0; i < len(a); i++ {
		result[i] = a[i] ^ b[i]
	}
	return result
}

// ComputeSignatureKey computes the ServerSignature key for SCRAM
func ComputeSignatureKey(saltedPassword []byte, clientMessage, serverMessage, clientFinalMessage string) []byte {
	serverKey := hmac.New(sha256.New, saltedPassword)
	serverKey.Write([]byte("Server Key"))

	authMessage := clientMessage + serverMessage + clientFinalMessage
	h := hmac.New(sha256.New, serverKey.Sum(nil))
	h.Write([]byte(authMessage))
	return h.Sum(nil)
}
