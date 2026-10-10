package crypt

import (
	"encoding/base64"
	"strings"
	"testing"
)

// Cross-language vector: encrypted by services/api/src/common/crypto.ts
// (Node) under this key. If this fails, API and worker have drifted apart —
// exactly the bug that shipped once before.
const (
	vectorKey   = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	vectorCT    = "IXeJMkN0uzlptJM3QAKI0CzyIbdOs028U612P5YxF+Ao1hEg5Kl+oyY="
	vectorPlain = "cross-check-1"
)

func TestDecryptAPIVector(t *testing.T) {
	key, err := ParseKey(vectorKey)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decrypt(key, vectorCT)
	if err != nil {
		t.Fatalf("decrypt API vector: %v", err)
	}
	if got != vectorPlain {
		t.Fatalf("got %q, want %q", got, vectorPlain)
	}
}

func TestRoundTrip(t *testing.T) {
	key, err := ParseKey(vectorKey)
	if err != nil {
		t.Fatal(err)
	}
	ct, err := Encrypt(key, "round me")
	if err != nil {
		t.Fatal(err)
	}
	// Layout: base64(iv(12) || tag(16) || ct) — same as crypto.ts.
	raw, err := base64.StdEncoding.DecodeString(ct)
	if err != nil || len(raw) < 28 {
		t.Fatalf("bad ciphertext encoding: %v", err)
	}
	got, err := Decrypt(key, ct)
	if err != nil {
		t.Fatal(err)
	}
	if got != "round me" {
		t.Fatalf("got %q", got)
	}
	if _, err := Decrypt(key, "not base64 !!"); err == nil {
		t.Fatal("expected error for garbage input")
	}
	short := base64.StdEncoding.EncodeToString(raw[:10])
	if _, err := Decrypt(key, short); err == nil || !strings.Contains(err.Error(), "too short") {
		t.Fatalf("expected too-short error, got %v", err)
	}
}
