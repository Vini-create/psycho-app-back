package auth

import (
	"bytes"
	"testing"
)

func TestSecretCipherRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for index := range key {
		key[index] = byte(31 - index)
	}

	cipher, err := NewSecretCipher(key)
	if err != nil {
		t.Fatalf("NewSecretCipher() error = %v", err)
	}

	want := []byte("webauthn-secret-value")
	encrypted, err := cipher.Encrypt(want)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if bytes.Contains(encrypted, want) {
		t.Fatal("encrypted secret contains plaintext")
	}

	got, err := cipher.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("Decrypt() = %q, want %q", got, want)
	}
}
