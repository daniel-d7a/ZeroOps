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

// Encrypt seals plaintext with a fresh random IV. Layout matches crypto.ts:
// iv || authTag || ciphertext — note GCM's own Seal appends the tag AFTER the
// ciphertext, so the three parts are rearranged on the way out.
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
	sealed := gcm.Seal(nil, iv, []byte(plaintext), nil) // ciphertext || tag
	tag := sealed[len(sealed)-gcm.Overhead():]
	ct := sealed[:len(sealed)-gcm.Overhead()]
	out := make([]byte, 0, len(iv)+len(tag)+len(ct))
	out = append(out, iv...)
	out = append(out, tag...)
	out = append(out, ct...)
	return base64.StdEncoding.EncodeToString(out), nil
}

// Decrypt opens a value written by the API (or by Encrypt above). The wire
// layout is iv || tag || ciphertext; GCM's Open wants the tag appended after
// the ciphertext, so rebuild that layout before calling it.
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
	header := gcm.NonceSize() + gcm.Overhead() // iv + tag
	if len(raw) < header {
		return "", fmt.Errorf("ciphertext too short")
	}
	iv := raw[:gcm.NonceSize()]
	tag := raw[gcm.NonceSize():header]
	ct := raw[header:]
	openInput := make([]byte, 0, len(ct)+len(tag))
	openInput = append(openInput, ct...)
	openInput = append(openInput, tag...)
	plain, err := gcm.Open(nil, iv, openInput, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plain), nil
}
