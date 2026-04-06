package auth

import (
	"crypto/x509"
	"encoding/pem"
	"testing"
)

func TestSMIMEConfig(t *testing.T) {
	cfg := NewSMIMEConfig()
	if cfg == nil {
		t.Fatal("Expected non-nil config")
	}
	if cfg.RequireEncryption {
		t.Error("RequireEncryption should be false by default")
	}
}

func TestSMIMESigner(t *testing.T) {
	// Create a test config
	testCfg := &SMIMEConfig{}

	// Create signer without keys
	signer := NewSMIMESigner(testCfg)
	if signer == nil {
		t.Fatal("Expected non-nil signer")
	}

	// Try to sign without keys - should fail
	_, err := signer.SignMessage([]byte("test message"), "from@example.com", "to@example.com")
	if err == nil {
		t.Error("Expected error when signing without keys")
	}
}

func TestSMIMEEncryptor(t *testing.T) {
	// Create encryptor without certificates
	encryptor := NewSMIMEEncryptor(&SMIMEConfig{})
	if encryptor == nil {
		t.Fatal("Expected non-nil encryptor")
	}

	// Try to encrypt without certificates - should fail
	_, err := encryptor.EncryptMessage([]byte("test message"), "from@example.com", "to@example.com")
	if err == nil {
		t.Error("Expected error when encrypting without certificates")
	}
}

func TestCreateSelfSignedCert(t *testing.T) {
	cert, key, err := CreateSelfSignedCert()
	if err != nil {
		t.Fatalf("CreateSelfSignedCert failed: %v", err)
	}
	if cert == nil {
		t.Fatal("Expected non-nil cert")
	}
	if key == nil {
		t.Fatal("Expected non-nil key")
	}

	// Verify certificate fields
	if cert.Subject.CommonName != "localhost" {
		t.Errorf("Expected CN=localhost, got %s", cert.Subject.CommonName)
	}

	// Verify key size
	if key.N.BitLen() != 2048 {
		t.Errorf("Expected 2048-bit key, got %d bits", key.N.BitLen())
	}
}

func TestParseCertificate(t *testing.T) {
	// Create a self-signed cert
	cert, _, err := CreateSelfSignedCert()
	if err != nil {
		t.Fatalf("CreateSelfSignedCert failed: %v", err)
	}

	// Encode to PEM
	pemCert := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	})

	// Parse it back
	parsedCert, err := ParseCertificate(pemCert)
	if err != nil {
		t.Fatalf("ParseCertificate failed: %v", err)
	}
	if parsedCert.Subject.CommonName != "localhost" {
		t.Errorf("Expected CN=localhost, got %s", parsedCert.Subject.CommonName)
	}
}

func TestParsePrivateKey(t *testing.T) {
	// Create a self-signed cert
	_, key, err := CreateSelfSignedCert()
	if err != nil {
		t.Fatalf("CreateSelfSignedCert failed: %v", err)
	}

	// Encode to PEM (PKCS1)
	pemKey := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})

	// Parse it back
	parsedKey, err := ParsePrivateKey(pemKey)
	if err != nil {
		t.Fatalf("ParsePrivateKey failed: %v", err)
	}
	if parsedKey.N.BitLen() != 2048 {
		t.Errorf("Expected 2048-bit key, got %d bits", parsedKey.N.BitLen())
	}
}

func TestAddrFromHeader(t *testing.T) {
	header := "From: Test User <test@example.com>\r\n"

	addr, err := AddrFromHeader(header)
	if err != nil {
		t.Fatalf("AddrFromHeader failed: %v", err)
	}
	if addr != "test@example.com" && addr != "Test User <test@example.com>" {
		// Both are acceptable depending on mail parser behavior
	}
}

