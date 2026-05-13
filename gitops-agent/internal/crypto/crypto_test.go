package crypto_test

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"io"
	"testing"

	"github.com/dada-tuda/console/gitops-agent/internal/crypto"
)

// encryptToken is a test helper that replicates what the backend does when storing tokens.
func encryptToken(keyHex string, plaintext string) ([]byte, error) {
	key, _ := hex.DecodeString(keyHex)
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	_, _ = io.ReadFull(rand.Reader, nonce)
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return ciphertext, nil
}

func TestDecryptToken_RoundTrip(t *testing.T) {
	keyHex := "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
	plain := "ghp_supersecrettoken123"

	ciphertext, err := encryptToken(keyHex, plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	got, err := crypto.DecryptToken(keyHex, ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != plain {
		t.Errorf("round-trip: got %q, want %q", got, plain)
	}
}

func TestDecryptToken_WrongKey(t *testing.T) {
	keyHex := "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
	wrongKey := "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	plain := "token"

	ciphertext, _ := encryptToken(keyHex, plain)

	_, err := crypto.DecryptToken(wrongKey, ciphertext)
	if err == nil {
		t.Error("expected error with wrong key, got nil")
	}
}

func TestDecryptToken_EmptyKey(t *testing.T) {
	_, err := crypto.DecryptToken("", []byte("anything"))
	if err == nil {
		t.Error("expected error with empty key")
	}
}

func TestDecryptToken_ShortCiphertext(t *testing.T) {
	keyHex := "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
	_, err := crypto.DecryptToken(keyHex, []byte("short"))
	if err == nil {
		t.Error("expected error with too-short ciphertext")
	}
}

func TestDecryptToken_BadKeyHex(t *testing.T) {
	_, err := crypto.DecryptToken("not-valid-hex!!", []byte("anything"))
	if err == nil {
		t.Error("expected error with invalid key hex")
	}
}
