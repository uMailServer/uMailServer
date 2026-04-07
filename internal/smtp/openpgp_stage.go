package smtp

import (
	"fmt"
	"strings"

	"github.com/umailserver/umailserver/internal/auth"
)

// OpenPGPStage implements OpenPGP verification and signing in the SMTP pipeline
type OpenPGPStage struct {
	keystore *OpenPGPKeystore
}

// OpenPGPKeystore manages OpenPGP keys per user
type OpenPGPKeystore struct {
	users map[string]*OpenPGPUserKeys
}

// OpenPGPUserKeys holds a user's OpenPGP keys
type OpenPGPUserKeys struct {
	PrivateKey []byte
	PublicKey  []byte
}

// NewOpenPGPStage creates a new OpenPGP pipeline stage
func NewOpenPGPStage(keystore *OpenPGPKeystore) *OpenPGPStage {
	return &OpenPGPStage{keystore: keystore}
}

// NewOpenPGPKeystore creates a new OpenPGP keystore
func NewOpenPGPKeystore() *OpenPGPKeystore {
	return &OpenPGPKeystore{
		users: make(map[string]*OpenPGPUserKeys),
	}
}

// GetKeys returns the OpenPGP keys for a user
func (ks *OpenPGPKeystore) GetKeys(user string) *OpenPGPUserKeys {
	if ks == nil {
		return nil
	}
	return ks.users[user]
}

// SetKeys sets the OpenPGP keys for a user
func (ks *OpenPGPKeystore) SetKeys(user string, keys *OpenPGPUserKeys) {
	if ks == nil {
		return
	}
	ks.users[user] = keys
}

func (s *OpenPGPStage) Name() string { return "OpenPGP" }

func (s *OpenPGPStage) Process(ctx *MessageContext) PipelineResult {
	// Check if message contains OpenPGP content
	contentType := getHeader(ctx.Headers, "Content-Type")

	if strings.Contains(contentType, "application/pgp-signature") {
		return s.verifyOpenPGP(ctx)
	}

	if strings.Contains(contentType, "application/pgp-encrypted") {
		return s.decryptOpenPGP(ctx)
	}

	return ResultAccept
}

// verifyOpenPGP verifies OpenPGP signed messages
func (s *OpenPGPStage) verifyOpenPGP(ctx *MessageContext) PipelineResult {
	// Get sender from envelope
	sender := ctx.From
	if sender == "" {
		return ResultAccept
	}

	// Extract user from sender
	user := extractUserFromRecipient(sender)
	if user == "" {
		return ResultAccept
	}

	// Get user's keys if available
	var keys *OpenPGPUserKeys
	if s.keystore != nil {
		keys = s.keystore.GetKeys(user)
	}

	if keys == nil {
		// No keys available for this user - can't verify
		ctx.SPFResult.Explanation = "OpenPGP signature present but no keys available for verification"
		return ResultAccept
	}

	// Create verifier with user's public key
	verifier := auth.NewOpenPGPVerifier(keys.PublicKey)
	if verifier == nil {
		return ResultAccept
	}

	// Verify the signature
	verified, err := verifier.VerifyMessage(ctx.Data)
	if err != nil {
		ctx.SPFResult.Explanation = fmt.Sprintf("OpenPGP verification failed: %v", err)
		return ResultAccept
	}

	if verified {
		ctx.SPFResult.Explanation = "OpenPGP signature verified"
	}

	return ResultAccept
}

// decryptOpenPGP decrypts OpenPGP encrypted messages
func (s *OpenPGPStage) decryptOpenPGP(ctx *MessageContext) PipelineResult {
	// Get recipient(s)
	if len(ctx.To) == 0 {
		return ResultAccept
	}

	// Use first recipient for key lookup
	recipient := ctx.To[0]
	user := extractUserFromRecipient(recipient)
	if user == "" {
		return ResultAccept
	}

	// Get user's keys if available
	var keys *OpenPGPUserKeys
	if s.keystore != nil {
		keys = s.keystore.GetKeys(user)
	}

	if keys == nil || len(keys.PrivateKey) == 0 {
		// No private key available - can't decrypt
		ctx.SPFResult.Explanation = "OpenPGP encrypted message but no private key available"
		ctx.Rejected = true
		ctx.RejectionCode = 550
		ctx.RejectionMessage = "Message is encrypted but no decryption key available"
		return ResultReject
	}

	// Create decryptor
	decryptor := auth.NewOpenPGPDecryptor(keys.PrivateKey)
	if decryptor == nil {
		return ResultAccept
	}

	// Decrypt
	decrypted, err := decryptor.DecryptMessage(ctx.Data)
	if err != nil {
		ctx.SPFResult.Explanation = fmt.Sprintf("OpenPGP decryption failed: %v", err)
		ctx.Rejected = true
		ctx.RejectionCode = 550
		ctx.RejectionMessage = "Failed to decrypt OpenPGP message"
		return ResultReject
	}

	// Update message data with decrypted content
	ctx.Data = decrypted
	ctx.SPFResult.Explanation = "OpenPGP message decrypted"

	return ResultAccept
}

// SignMessage signs a message using OpenPGP for a user
func (s *OpenPGPStage) SignMessage(user string, from, to string, data []byte) ([]byte, error) {
	if s.keystore == nil {
		return nil, fmt.Errorf("OpenPGP keystore not available")
	}

	keys := s.keystore.GetKeys(user)
	if keys == nil {
		return nil, fmt.Errorf("no OpenPGP keys for user: %s", user)
	}

	signer := auth.NewOpenPGPSigner(keys.PrivateKey)
	return signer.SignMessage(data, from, to)
}

// EncryptMessage encrypts a message using OpenPGP for a user
func (s *OpenPGPStage) EncryptMessage(user string, from, to string, data []byte) ([]byte, error) {
	if s.keystore == nil {
		return nil, fmt.Errorf("OpenPGP keystore not available")
	}

	keys := s.keystore.GetKeys(user)
	if keys == nil {
		return nil, fmt.Errorf("no OpenPGP keys for user: %s", user)
	}

	encryptor := auth.NewOpenPGPEncryptor([][]byte{keys.PublicKey})
	return encryptor.EncryptMessage(data, from, to)
}
