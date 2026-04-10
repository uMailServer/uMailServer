package smtp

import (
	"testing"
)

// --- SMIMEKeystore tests ---

func TestNewSMIMEKeystore(t *testing.T) {
	ks := NewSMIMEKeystore()
	if ks == nil {
		t.Fatal("Expected non-nil SMIMEKeystore")
	}
	if ks.users == nil {
		t.Error("Expected users map to be initialized")
	}
}

func TestNewSMIMEStage(t *testing.T) {
	ks := NewSMIMEKeystore()
	stage := NewSMIMEStage(ks)
	if stage == nil {
		t.Fatal("Expected non-nil SMIMEStage")
	}
	if stage.keystore != ks {
		t.Error("Keystore not set correctly")
	}
}

func TestNewSMIMEStage_NilKeystore(t *testing.T) {
	stage := NewSMIMEStage(nil)
	if stage == nil {
		t.Fatal("Expected non-nil SMIMEStage")
	}
	if stage.keystore != nil {
		t.Error("Expected nil keystore")
	}
}

func TestSMIMEStage_Name(t *testing.T) {
	stage := NewSMIMEStage(nil)
	if stage.Name() != "S/MIME" {
		t.Errorf("Expected 'S/MIME', got %q", stage.Name())
	}
}

func TestSMIMEKeystore_GetKeys_Nil(t *testing.T) {
	var ks *SMIMEKeystore
	result := ks.GetKeys("user")
	if result != nil {
		t.Error("Expected nil for nil keystore")
	}
}

func TestSMIMEKeystore_GetKeys_NotFound(t *testing.T) {
	ks := NewSMIMEKeystore()
	result := ks.GetKeys("nonexistent")
	if result != nil {
		t.Error("Expected nil for nonexistent user")
	}
}

func TestSMIMEKeystore_GetKeys_Found(t *testing.T) {
	ks := NewSMIMEKeystore()
	keys := &SMIMEUserKeys{
		SigningCert:    []byte("cert"),
		SigningKey:     []byte("key"),
		EncryptionCert: []byte("enc"),
	}
	ks.SetKeys("user@example.com", keys)

	result := ks.GetKeys("user@example.com")
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if string(result.SigningCert) != "cert" {
		t.Errorf("Expected cert 'cert', got %q", string(result.SigningCert))
	}
	if string(result.SigningKey) != "key" {
		t.Errorf("Expected key 'key', got %q", string(result.SigningKey))
	}
}

func TestSMIMEKeystore_SetKeys_Nil(t *testing.T) {
	var ks *SMIMEKeystore
	keys := &SMIMEUserKeys{}
	// Should not panic
	ks.SetKeys("user", keys)
}

func TestSMIMEKeystore_SetKeys_Update(t *testing.T) {
	ks := NewSMIMEKeystore()
	keys1 := &SMIMEUserKeys{SigningKey: []byte("key1")}
	keys2 := &SMIMEUserKeys{SigningKey: []byte("key2")}

	ks.SetKeys("user", keys1)
	result1 := ks.GetKeys("user")
	if string(result1.SigningKey) != "key1" {
		t.Errorf("Expected key1, got %q", string(result1.SigningKey))
	}

	ks.SetKeys("user", keys2)
	result2 := ks.GetKeys("user")
	if string(result2.SigningKey) != "key2" {
		t.Errorf("Expected key2, got %q", string(result2.SigningKey))
	}
}

// --- getHeader tests ---

func TestGetHeader_NilHeaders(t *testing.T) {
	result := getHeader(nil, "Content-Type")
	if result != "" {
		t.Errorf("Expected empty string, got %q", result)
	}
}

func TestGetHeader_EmptyHeaders(t *testing.T) {
	result := getHeader(map[string][]string{}, "Content-Type")
	if result != "" {
		t.Errorf("Expected empty string, got %q", result)
	}
}

func TestGetHeader_NotFound(t *testing.T) {
	headers := map[string][]string{
		"From": {"sender@example.com"},
	}
	result := getHeader(headers, "Content-Type")
	if result != "" {
		t.Errorf("Expected empty string, got %q", result)
	}
}

func TestGetHeader_Found(t *testing.T) {
	headers := map[string][]string{
		"Content-Type": {"text/plain"},
		"From":         {"sender@example.com"},
	}
	result := getHeader(headers, "Content-Type")
	if result != "text/plain" {
		t.Errorf("Expected 'text/plain', got %q", result)
	}
}

func TestGetHeader_MultipleValues(t *testing.T) {
	headers := map[string][]string{
		"Content-Type": {"text/plain", "text/html"},
	}
	result := getHeader(headers, "Content-Type")
	if result != "text/plain" {
		t.Errorf("Expected first value 'text/plain', got %q", result)
	}
}

func TestGetHeader_CaseInsensitive(t *testing.T) {
	headers := map[string][]string{
		"content-type": {"text/plain"},
	}
	result := getHeader(headers, "Content-Type")
	// getHeader does exact match, so this won't find it
	if result != "" {
		t.Errorf("Expected empty string for case mismatch, got %q", result)
	}
}

// --- SMIMEUserKeys struct ---

func TestSMIMEUserKeys_Fields(t *testing.T) {
	keys := &SMIMEUserKeys{
		SigningCert:    []byte("sign-cert"),
		SigningKey:     []byte("sign-key"),
		EncryptionCert: []byte("enc-cert"),
	}

	if string(keys.SigningCert) != "sign-cert" {
		t.Errorf("SigningCert mismatch")
	}
	if string(keys.SigningKey) != "sign-key" {
		t.Errorf("SigningKey mismatch")
	}
	if string(keys.EncryptionCert) != "enc-cert" {
		t.Errorf("EncryptionCert mismatch")
	}
}

// --- Process method with different content types ---

func TestSMIMEStage_Process_NonSMIMEContent(t *testing.T) {
	stage := NewSMIMEStage(NewSMIMEKeystore())
	ctx := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{
			"Content-Type": {"text/plain"},
		},
		Data: []byte("Hello"),
	}

	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept for non-SMIME content, got %v", result)
	}
}

func TestSMIMEStage_Process_PKCS7Signature(t *testing.T) {
	stage := NewSMIMEStage(NewSMIMEKeystore())
	ctx := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{
			"Content-Type": {"application/pkcs7-signature"},
		},
		Data: []byte("Signed content"),
	}

	result := stage.Process(ctx)
	// verifySMIME will accept since no keys available
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %v", result)
	}
}

func TestSMIMEStage_Process_PKCS7Mime(t *testing.T) {
	stage := NewSMIMEStage(NewSMIMEKeystore())
	ctx := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{
			"Content-Type": {"application/pkcs7-mime"},
		},
		Data: []byte("Encrypted content"),
	}

	result := stage.Process(ctx)
	// decryptSMIME will reject since no keys available
	if result != ResultReject {
		t.Errorf("Expected ResultReject for encrypted without keys, got %v", result)
	}
}

func TestSMIMEStage_Process_XPKCS7Signature(t *testing.T) {
	stage := NewSMIMEStage(NewSMIMEKeystore())
	ctx := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{
			"Content-Type": {"application/x-pkcs7-signature"},
		},
		Data: []byte("Signed content"),
	}

	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %v", result)
	}
}

func TestSMIMEStage_Process_XPKCS7Mime(t *testing.T) {
	stage := NewSMIMEStage(NewSMIMEKeystore())
	ctx := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{
			"Content-Type": {"application/x-pkcs7-mime"},
		},
		Data: []byte("Encrypted content"),
	}

	result := stage.Process(ctx)
	if result != ResultReject {
		t.Errorf("Expected ResultReject, got %v", result)
	}
}

// --- verifySMIME with keys ---

func TestSMIMEStage_VerifySMIME_WithKeys(t *testing.T) {
	ks := NewSMIMEKeystore()
	// Set up keys (though verification will likely fail without valid certs)
	ks.SetKeys("sender", &SMIMEUserKeys{
		SigningCert: []byte("invalid-cert"),
		SigningKey:  []byte("invalid-key"),
	})

	stage := NewSMIMEStage(ks)
	ctx := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{
			"Content-Type": {"application/pkcs7-signature"},
		},
		Data: []byte("Signed content"),
	}

	result := stage.Process(ctx)
	// Should accept even with invalid keys (marks as unverified)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %v", result)
	}
}

// --- decryptSMIME with keys but no private key ---

func TestSMIMEStage_DecryptSMIME_WithEncryptionCertOnly(t *testing.T) {
	ks := NewSMIMEKeystore()
	// Set up only encryption cert (no private key)
	ks.SetKeys("recipient", &SMIMEUserKeys{
		EncryptionCert: []byte("some-cert"),
		SigningKey:     nil, // No private key
	})

	stage := NewSMIMEStage(ks)
	ctx := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{
			"Content-Type": {"application/pkcs7-mime"},
		},
		Data: []byte("Encrypted content"),
	}

	result := stage.Process(ctx)
	// Should reject - no private key to decrypt
	if result != ResultReject {
		t.Errorf("Expected ResultReject for missing private key, got %v", result)
	}
	if !ctx.Rejected {
		t.Error("Expected ctx.Rejected to be true")
	}
	if ctx.RejectionCode != 550 {
		t.Errorf("Expected rejection code 550, got %d", ctx.RejectionCode)
	}
}

// --- SignMessage without keystore ---

func TestSMIMEStage_SignMessage_NoKeystore(t *testing.T) {
	stage := NewSMIMEStage(nil)

	_, err := stage.SignMessage("user", "from@test.com", "to@test.com", []byte("data"))
	if err == nil {
		t.Error("Expected error when keystore is nil")
	}
}

// --- EncryptMessage without keystore ---

func TestSMIMEStage_EncryptMessage_NoKeystore(t *testing.T) {
	stage := NewSMIMEStage(nil)

	_, err := stage.EncryptMessage("user", "from@test.com", "to@test.com", []byte("data"))
	if err == nil {
		t.Error("Expected error when keystore is nil")
	}
}

// --- SignMessage without user keys ---

func TestSMIMEStage_SignMessage_NoKeys(t *testing.T) {
	ks := NewSMIMEKeystore()
	stage := NewSMIMEStage(ks)

	_, err := stage.SignMessage("nonexistent", "from@test.com", "to@test.com", []byte("data"))
	if err == nil {
		t.Error("Expected error when user has no keys")
	}
}

// --- EncryptMessage without user keys ---

func TestSMIMEStage_EncryptMessage_NoKeys(t *testing.T) {
	ks := NewSMIMEKeystore()
	stage := NewSMIMEStage(ks)

	_, err := stage.EncryptMessage("nonexistent", "from@test.com", "to@test.com", []byte("data"))
	if err == nil {
		t.Error("Expected error when user has no keys")
	}
}

// --- verifySMIME with empty sender ---

func TestSMIMEStage_VerifySMIME_EmptySender(t *testing.T) {
	stage := NewSMIMEStage(NewSMIMEKeystore())
	ctx := &MessageContext{
		From:    "", // Empty sender
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{
			"Content-Type": {"application/pkcs7-signature"},
		},
		Data: []byte("Signed content"),
	}

	result := stage.Process(ctx)
	// Should accept but skip verification
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %v", result)
	}
}

// --- decryptSMIME with no recipients ---

func TestSMIMEStage_DecryptSMIME_NoRecipients(t *testing.T) {
	stage := NewSMIMEStage(NewSMIMEKeystore())
	ctx := &MessageContext{
		From:    "sender@example.com",
		To:      []string{}, // No recipients
		Headers: map[string][]string{
			"Content-Type": {"application/pkcs7-mime"},
		},
		Data: []byte("Encrypted content"),
	}

	result := stage.Process(ctx)
	// Should accept with no recipients
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %v", result)
	}
}

// --- extractUserFromRecipient tests ---

func TestExtractUserFromRecipient_Email(t *testing.T) {
	result := extractUserFromRecipient("user@example.com")
	if result != "user" {
		t.Errorf("Expected 'user', got %q", result)
	}
}

func TestExtractUserFromRecipient_Empty(t *testing.T) {
	result := extractUserFromRecipient("")
	if result != "" {
		t.Errorf("Expected empty string, got %q", result)
	}
}

func TestExtractUserFromRecipient_BangFormat(t *testing.T) {
	result := extractUserFromRecipient("user!otherdomain!mailbox")
	if result != "user" {
		t.Errorf("Expected 'user', got %q", result)
	}
}

func TestExtractUserFromRecipient_NoAt(t *testing.T) {
	result := extractUserFromRecipient("username")
	if result != "username" {
		t.Errorf("Expected 'username', got %q", result)
	}
}

// --- Process with empty headers ---

func TestSMIMEStage_Process_EmptyHeaders(t *testing.T) {
	stage := NewSMIMEStage(NewSMIMEKeystore())
	ctx := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{},
		Data:    []byte("Hello"),
	}

	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept for empty headers, got %v", result)
	}
}

// --- Process with nil headers ---

func TestSMIMEStage_Process_NilHeaders(t *testing.T) {
	stage := NewSMIMEStage(NewSMIMEKeystore())
	ctx := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: nil,
		Data:    []byte("Hello"),
	}

	result := stage.Process(ctx)
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept for nil headers, got %v", result)
	}
}

// --- Process with multiple content types ---

func TestSMIMEStage_Process_MultipleContentTypes(t *testing.T) {
	stage := NewSMIMEStage(NewSMIMEKeystore())
	ctx := &MessageContext{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Headers: map[string][]string{
			"Content-Type": {"text/plain; boundary=----"},
		},
		Data: []byte("Hello"),
	}

	result := stage.Process(ctx)
	// Should not match pkcs7 patterns
	if result != ResultAccept {
		t.Errorf("Expected ResultAccept, got %v", result)
	}
}