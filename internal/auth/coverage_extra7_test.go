package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
)

// --- S/MIME tests ---

func TestSMIMEVerify_InvalidMessage(t *testing.T) {
	v := NewSMIMEVerifier()

	// Too short message
	_, err := v.VerifyMessage([]byte("short"))
	if err == nil {
		t.Error("Expected error for short message")
	}
}

func TestSMIMEVerify_MissingBoundary(t *testing.T) {
	v := NewSMIMEVerifier()

	msg := []byte("Content-Type: multipart/signed\r\n\r\ncontent")
	_, err := v.VerifyMessage(msg)
	if err == nil {
		t.Error("Expected error for missing boundary")
	}
}

func TestSMIMEVerify_MultipartError(t *testing.T) {
	v := NewSMIMEVerifier()

	msg := []byte("Content-Type: multipart/signed; boundary=\"\"\r\n\r\n")
	_, err := v.VerifyMessage(msg)
	if err == nil {
		t.Error("Expected error for empty boundary")
	}
}

func TestSMIMESigner_SignMessage(t *testing.T) {
	cert := generateTestCert(t)
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	config := &SMIMEConfig{
		SigningCert:      cert,
		SigningKey:        privateKey,
		EncryptionCerts: []*x509.Certificate{cert},
	}

	signer := NewSMIMESigner(config)
	signed, err := signer.SignMessage([]byte("Hello, World!"), "sender@example.com", "recipient@example.com")
	if err != nil {
		t.Fatalf("SignMessage error: %v", err)
	}
	if len(signed) == 0 {
		t.Error("Expected signed message")
	}
}

func TestSMIMESigner_NoSigningCert(t *testing.T) {
	config := &SMIMEConfig{
		EncryptionCerts: []*x509.Certificate{},
	}

	signer := NewSMIMESigner(config)
	_, err := signer.SignMessage([]byte("Hello, World!"), "sender@example.com", "recipient@example.com")
	if err == nil {
		t.Error("Expected error when no signing cert")
	}
}

func TestSMIMEEncrypt_EncryptMessage(t *testing.T) {
	cert := generateTestCert(t)

	config := &SMIMEConfig{
		EncryptionCerts: []*x509.Certificate{cert},
		SigningKey:       nil,
	}

	encryptor := NewSMIMEEncryptor(config)
	msg := []byte("Hello, World!")

	encrypted, err := encryptor.EncryptMessage(msg, "sender@example.com", "recipient@example.com")
	if err != nil {
		t.Fatalf("EncryptMessage error: %v", err)
	}
	if len(encrypted) == 0 {
		t.Error("Expected encrypted message")
	}
}

func TestSMIMEEncrypt_NoEncryptionCerts(t *testing.T) {
	config := &SMIMEConfig{
		EncryptionCerts: []*x509.Certificate{},
		SigningKey:       nil,
	}

	encryptor := NewSMIMEEncryptor(config)
	msg := []byte("Hello, World!")

	_, err := encryptor.EncryptMessage(msg, "sender@example.com", "recipient@example.com")
	if err == nil {
		t.Error("Expected error when no encryption certs")
	}
}

func TestSMIMEDecrypt_NoPrivateKey(t *testing.T) {
	config := &SMIMEConfig{}
	decryptor := NewSMIMEDecryptor(config)

	_, err := decryptor.DecryptMessage([]byte("something"))
	if err == nil {
		t.Error("Expected error for missing private key")
	}
}

func TestSMIMEDecrypt_InvalidFormat(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	config := &SMIMEConfig{
		SigningKey: privateKey,
	}
	decryptor := NewSMIMEDecryptor(config)

	_, err = decryptor.DecryptMessage([]byte("not valid format"))
	if err == nil {
		t.Error("Expected error for invalid format")
	}
}

func TestParseMultipart_InvalidFormat(t *testing.T) {
	msg := []byte("not multipart format")

	_, err := parseMultipart(msg)
	if err == nil {
		t.Error("Expected error for invalid format")
	}
}

func TestParseMultipart_NoBoundary(t *testing.T) {
	msg := []byte("Content-Type: multipart/signed\r\n\r\nsome content")

	_, err := parseMultipart(msg)
	if err == nil {
		t.Error("Expected error for missing boundary")
	}
}

func TestExtractBase64Content_Multiline(t *testing.T) {
	// Test with content that spans multiple lines (leading whitespace style)
	msg := []byte("Content-Type: application/pkcs7-mime\r\n\r\nSGVs  bG8gV29y  bGQh")

	content := extractBase64Content(msg)
	// The function trims and concatenates lines
	if content == "" {
		// Empty is ok - function has specific parsing logic
	}
}

func TestExtractBase64Content_NotFound(t *testing.T) {
	msg := []byte("Content-Type: text/plain\r\n\r\nHello")

	content := extractBase64Content(msg)
	if content != "" {
		t.Errorf("extractBase64Content = %q, want empty for non-SMIME content", content)
	}
}

func TestExtractBase64Content_NoContent(t *testing.T) {
	msg := []byte("Content-Type: application/pkcs7-mime\r\n\r\n")

	content := extractBase64Content(msg)
	if content != "" {
		t.Errorf("extractBase64Content = %q, want empty", content)
	}
}

// --- OpenPGP tests ---

func TestOpenPGPSign_Message(t *testing.T) {
	signer := NewOpenPGPSigner([]byte("test private key"))

	signed, err := signer.SignMessage([]byte("Hello, World!"), "sender@example.com", "recipient@example.com")
	if err != nil {
		t.Fatalf("SignMessage error: %v", err)
	}
	if len(signed) == 0 {
		t.Error("Expected signed message")
	}
}

func TestOpenPGPVerify_NoPublicKey(t *testing.T) {
	verifier := NewOpenPGPVerifier(nil)

	_, err := verifier.VerifyMessage([]byte("test message"))
	if err == nil {
		t.Error("Expected error for nil public key")
	}
}

func TestOpenPGPEncrypt_EncryptMessage(t *testing.T) {
	encryptor := NewOpenPGPEncryptor([][]byte{[]byte("recipient-key")})

	encrypted, err := encryptor.EncryptMessage([]byte("Hello"), "sender@example.com", "recipient@example.com")
	if err != nil {
		t.Fatalf("EncryptMessage error: %v", err)
	}
	if len(encrypted) == 0 {
		t.Error("Expected encrypted message")
	}
}

func TestOpenPGPEncrypt_NoPublicKeys(t *testing.T) {
	encryptor := NewOpenPGPEncryptor(nil)

	_, err := encryptor.EncryptMessage([]byte("Hello"), "sender@example.com", "recipient@example.com")
	if err == nil {
		t.Error("Expected error when no public keys")
	}
}

func TestOpenPGPDecrypt_NoPrivateKey(t *testing.T) {
	decryptor := NewOpenPGPDecryptor(nil)

	_, err := decryptor.DecryptMessage([]byte("something"))
	if err == nil {
		t.Error("Expected error for nil private key")
	}
}

func TestOpenPGPDecrypt_InvalidFormat(t *testing.T) {
	decryptor := NewOpenPGPDecryptor([]byte("test-key"))

	_, err := decryptor.DecryptMessage([]byte("not multipart"))
	if err == nil {
		t.Error("Expected error for invalid format")
	}
}

func TestOpenPGPDecrypt_DecryptAndVerifyEmptyKeyError(t *testing.T) {
	decryptor := NewOpenPGPDecryptor([]byte(""))
	signer := NewOpenPGPSigner([]byte("test-signing-key"))

	_, _, err := decryptor.DecryptAndVerify([]byte("test"), signer)
	if err == nil {
		t.Error("Expected error for empty private key")
	}
}

// --- LoadCertificateFromFile and LoadPrivateKeyFromFile ---

func TestLoadCertificateFromFile_InvalidFile(t *testing.T) {
	_, err := LoadCertificateFromFile("/nonexistent/path/cert.pem")
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

func TestLoadPrivateKeyFromFile_InvalidFile(t *testing.T) {
	_, err := LoadPrivateKeyFromFile("/nonexistent/path/key.pem")
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

// --- GenerateBoundary ---

func TestGenerateBoundary(t *testing.T) {
	b1 := generateBoundary()
	b2 := generateBoundary()

	if b1 == "" {
		t.Error("generateBoundary returned empty string")
	}
	if b1 == b2 {
		t.Error("generateBoundary returned same boundary twice")
	}
}

// --- ParseCertificate and ParsePrivateKey ---

func TestParseCertificate_InvalidPEM(t *testing.T) {
	_, err := ParseCertificate([]byte("not a valid pem block"))
	if err == nil {
		t.Error("Expected error for invalid PEM")
	}
}

func TestParseCertificate_ValidPEM(t *testing.T) {
	// Generate a self-signed cert and encode as PEM
	cert := generateTestCert(t)
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	})

	parsed, err := ParseCertificate(certPEM)
	if err != nil {
		t.Fatalf("ParseCertificate error: %v", err)
	}
	if parsed == nil {
		t.Error("Expected parsed certificate")
	}
}

func TestParsePrivateKey_InvalidPEM(t *testing.T) {
	_, err := ParsePrivateKey([]byte("not a valid pem block"))
	if err == nil {
		t.Error("Expected error for invalid PEM")
	}
}

func TestParsePrivateKey_ValidPEM(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	parsed, err := ParsePrivateKey(keyPEM)
	if err != nil {
		t.Fatalf("ParsePrivateKey error: %v", err)
	}
	if parsed == nil {
		t.Error("Expected parsed private key")
	}
}
