package push

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratedVAPIDKeysCanSign(t *testing.T) {
	check := func(privateKey, publicKey string) {
		t.Helper()
		private, err := base64.RawURLEncoding.DecodeString(privateKey)
		if err != nil {
			t.Fatal(err)
		}
		if len(private) != 32 {
			t.Fatalf("VAPID private key must be a 32-byte scalar, got %d bytes", len(private))
		}
		public, err := base64.RawURLEncoding.DecodeString(publicKey)
		if err != nil {
			t.Fatal(err)
		}
		validatedPublic, err := ecdh.P256().NewPublicKey(public)
		if err != nil {
			t.Fatal(err)
		}
		encodedPublic := validatedPublic.Bytes()
		x, y := new(big.Int).SetBytes(encodedPublic[1:33]), new(big.Int).SetBytes(encodedPublic[33:])
		key := &ecdsa.PrivateKey{
			PublicKey: ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y},
			D:         new(big.Int).SetBytes(private),
		}
		digest := sha256.Sum256([]byte("ordinary notification"))
		signature, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
		if err != nil {
			t.Fatal(err)
		}
		if !ecdsa.VerifyASN1(&key.PublicKey, digest[:], signature) {
			t.Fatal("private scalar does not match the advertised public key")
		}
	}
	for i := 0; i < 3; i++ {
		private, public, err := generateVAPIDKeys()
		if err != nil {
			t.Fatal(err)
		}
		check(private, public)
	}
	dir := t.TempDir()
	service, err := NewService(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	check(service.config.VAPIDPrivateKey, service.config.VAPIDPublicKey)
	reloaded, err := NewService(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if service.config != reloaded.config {
		t.Fatal("persisted keypair changed on reload")
	}
	check(reloaded.config.VAPIDPrivateKey, reloaded.config.VAPIDPublicKey)
	private, err := base64.RawURLEncoding.DecodeString(service.config.VAPIDPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	validatedPrivate, err := ecdh.P256().NewPrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	encodedPublic := validatedPrivate.PublicKey().Bytes()
	x, y := new(big.Int).SetBytes(encodedPublic[1:33]), new(big.Int).SetBytes(encodedPublic[33:])
	key := &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, D: new(big.Int).SetBytes(private)}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	legacyConfig := service.config
	legacyConfig.VAPIDPrivateKey = base64.RawURLEncoding.EncodeToString(der)
	data, err := json.Marshal(legacyConfig)
	if err != nil {
		t.Fatal(err)
	}
	legacyDir := t.TempDir()
	path := filepath.Join(legacyDir, "vapid.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	normalized, err := NewService(legacyDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.config != service.config {
		t.Fatal("legacy normalization changed key identity or subject")
	}
	check(normalized.config.VAPIDPrivateKey, normalized.config.VAPIDPublicKey)
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var persisted Config
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted != normalized.config {
		t.Fatal("normalized scalar was not persisted")
	}
}
