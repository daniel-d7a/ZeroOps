// Package crypt implements the platform's value encryption: AES-256-GCM
// (plan §7). One format, byte-compatible with the API's common/crypto.ts:
//
//	base64( iv(12) || authTag(16) || ciphertext )
//
// The key comes from ENCRYPTION_KEY (64 hex chars = 32 bytes), parsed and
// validated once in config — this package never reads the environment.
package crypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
)

// ParseKey decodes the 64-hex-char ENCRYPTION_KEY. Called once at startup;
// an invalid key must crash the worker, not surface as a decrypt failure
// on some job hours later.
func ParseKey(hexKey string) ([]byte, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("ENCRYPTION_KEY must be 64 hex characters (32 bytes)")
	}
	return key, nil
}

// Encrypt seals plaintext with a fresh random IV.
func Encrypt(key []byte, plaintext string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	iv := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", err
	}
	sealed := gcm.Seal(iv, iv, []byte(plaintext), nil) // nonce || ciphertext+tag
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt opens a value written by the API (or by Encrypt above).
func Decrypt(key []byte, encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("ciphertext too short")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plain), nil
}
