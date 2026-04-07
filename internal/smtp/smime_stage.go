package smtp

import (
	"crypto/x509"
	"fmt"
	"strings"

	"github.com/umailserver/umailserver/internal/auth"
)

// SMIMEStage implements S/MIME verification and signing in the SMTP pipeline
type SMIMEStage struct {
	keystore *SMIMEKeystore
}

// SMIMEKeystore manages S/MIME keys per user
type SMIMEKeystore struct {
	users map[string]*SMIMEUserKeys
}

// SMIMEUserKeys holds a user's S/MIME keys
type SMIMEUserKeys struct {
	SigningCert    []byte
	SigningKey     []byte
	EncryptionCert []byte
}

// NewSMIMEStage creates a new S/MIME pipeline stage
func NewSMIMEStage(keystore *SMIMEKeystore) *SMIMEStage {
	return &SMIMEStage{keystore: keystore}
}

// NewSMIMEKeystore creates a new S/MIME keystore
func NewSMIMEKeystore() *SMIMEKeystore {
	return &SMIMEKeystore{
		users: make(map[string]*SMIMEUserKeys),
	}
}

// GetKeys returns the S/MIME keys for a user
func (ks *SMIMEKeystore) GetKeys(user string) *SMIMEUserKeys {
	if ks == nil {
		return nil
	}
	return ks.users[user]
}

// SetKeys sets the S/MIME keys for a user
func (ks *SMIMEKeystore) SetKeys(user string, keys *SMIMEUserKeys) {
	if ks == nil {
		return
	}
	ks.users[user] = keys
}

func (s *SMIMEStage) Name() string { return "S/MIME" }

func (s *SMIMEStage) Process(ctx *MessageContext) PipelineResult {
	// Check if message contains S/MIME content
	contentType := getHeader(ctx.Headers, "Content-Type")

	if strings.Contains(contentType, "application/pkcs7-signature") ||
		strings.Contains(contentType, "application/x-pkcs7-signature") {
		return s.verifySMIME(ctx)
	}

	if strings.Contains(contentType, "application/pkcs7-mime") ||
		strings.Contains(contentType, "application/x-pkcs7-mime") {
		return s.decryptSMIME(ctx)
	}

	return ResultAccept
}

// verifySMIME verifies S/MIME signed messages
func (s *SMIMEStage) verifySMIME(ctx *MessageContext) PipelineResult {
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
	var keys *SMIMEUserKeys
	if s.keystore != nil {
		keys = s.keystore.GetKeys(user)
	}

	if keys == nil {
		// No keys available for this user - can't verify
		// Mark as unverified but accept (legacy behavior)
		ctx.SPFResult.Explanation = "S/MIME signature present but no keys available for verification"
		return ResultAccept
	}

	// Create verifier with user's public key
	verifier := auth.NewSMIMEVerifier()
	if verifier == nil {
		return ResultAccept
	}

	// Verify the signature
	verified, err := verifier.VerifyMessage(ctx.Data)
	if err != nil {
		// Verification failed
		ctx.SPFResult.Explanation = fmt.Sprintf("S/MIME verification failed: %v", err)
		// Don't reject - allow but flag
		return ResultAccept
	}

	if verified {
		ctx.SPFResult.Explanation = "S/MIME signature verified"
	}

	return ResultAccept
}

// decryptSMIME decrypts S/MIME encrypted messages
func (s *SMIMEStage) decryptSMIME(ctx *MessageContext) PipelineResult {
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
	var keys *SMIMEUserKeys
	if s.keystore != nil {
		keys = s.keystore.GetKeys(user)
	}

	if keys == nil || len(keys.SigningKey) == 0 {
		// No private key available - can't decrypt
		ctx.SPFResult.Explanation = "S/MIME encrypted message but no private key available"
		// Reject with specific error
		ctx.Rejected = true
		ctx.RejectionCode = 550
		ctx.RejectionMessage = "Message is encrypted but no decryption key available"
		return ResultReject
	}

	// Create decryptor
	smimeConfig := auth.NewSMIMEConfig()
	smimeConfig.SigningKey = keys.SigningKey
	if len(keys.EncryptionCert) > 0 {
		cert, _ := auth.ParseCertificate(keys.EncryptionCert)
		if cert != nil {
			smimeConfig.EncryptionCerts = []*x509.Certificate{cert}
		}
	}

	decryptor := auth.NewSMIMEDecryptor(smimeConfig)
	if decryptor == nil {
		return ResultAccept
	}

	// Decrypt
	decrypted, err := decryptor.DecryptMessage(ctx.Data)
	if err != nil {
		ctx.SPFResult.Explanation = fmt.Sprintf("S/MIME decryption failed: %v", err)
		ctx.Rejected = true
		ctx.RejectionCode = 550
		ctx.RejectionMessage = "Failed to decrypt S/MIME message"
		return ResultReject
	}

	// Update message data with decrypted content
	ctx.Data = decrypted
	ctx.SPFResult.Explanation = "S/MIME message decrypted"

	return ResultAccept
}

// SignMessage signs a message using S/MIME for a user
func (s *SMIMEStage) SignMessage(user string, from, to string, data []byte) ([]byte, error) {
	if s.keystore == nil {
		return nil, fmt.Errorf("S/MIME keystore not available")
	}

	keys := s.keystore.GetKeys(user)
	if keys == nil {
		return nil, fmt.Errorf("no S/MIME keys for user: %s", user)
	}

	smimeConfig := auth.NewSMIMEConfig()
	if len(keys.SigningCert) > 0 {
		cert, _ := auth.ParseCertificate(keys.SigningCert)
		if cert != nil {
			smimeConfig.SigningCert = cert
		}
	}
	smimeConfig.SigningKey = keys.SigningKey

	signer := auth.NewSMIMESigner(smimeConfig)
	return signer.SignMessage(data, from, to)
}

// EncryptMessage encrypts a message using S/MIME for a user
func (s *SMIMEStage) EncryptMessage(user string, from, to string, data []byte) ([]byte, error) {
	if s.keystore == nil {
		return nil, fmt.Errorf("S/MIME keystore not available")
	}

	keys := s.keystore.GetKeys(user)
	if keys == nil {
		return nil, fmt.Errorf("no S/MIME keys for user: %s", user)
	}

	smimeConfig := auth.NewSMIMEConfig()
	smimeConfig.SigningKey = keys.SigningKey
	if len(keys.EncryptionCert) > 0 {
		cert, _ := auth.ParseCertificate(keys.EncryptionCert)
		if cert != nil {
			smimeConfig.EncryptionCerts = []*x509.Certificate{cert}
		}
	}

	encryptor := auth.NewSMIMEEncryptor(smimeConfig)
	return encryptor.EncryptMessage(data, from, to)
}

func getHeader(headers map[string][]string, key string) string {
	if headers == nil {
		return ""
	}
	vals := headers[key]
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}
