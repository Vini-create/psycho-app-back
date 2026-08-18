package auth

import (
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
)

func TestNewPasskeyManagerUsesStrictSecurityDefaults(t *testing.T) {
	cipher, err := NewSecretCipher(make([]byte, 32))
	if err != nil {
		t.Fatalf("NewSecretCipher() error = %v", err)
	}

	manager, err := NewPasskeyManager(
		NewRepository(nil),
		cipher,
		PasskeyConfig{
			RPID:          "localhost",
			RPDisplayName: "Anamnesys",
			RPOrigins:     []string{"http://localhost:3000"},
			CeremonyTTL:   5 * time.Minute,
		},
	)
	if err != nil {
		t.Fatalf("NewPasskeyManager() error = %v", err)
	}

	config := manager.webAuthn.Config
	if config.RPAllowCrossOrigin {
		t.Fatal("RPAllowCrossOrigin = true, want false")
	}
	if config.AttestationPreference != protocol.PreferNoAttestation {
		t.Fatalf("AttestationPreference = %q, want %q", config.AttestationPreference, protocol.PreferNoAttestation)
	}
	if config.AuthenticatorSelection.UserVerification != protocol.VerificationRequired {
		t.Fatalf(
			"UserVerification = %q, want %q",
			config.AuthenticatorSelection.UserVerification,
			protocol.VerificationRequired,
		)
	}
	if !config.Timeouts.Login.Enforce || !config.Timeouts.Registration.Enforce {
		t.Fatal("WebAuthn server-side timeouts must be enforced")
	}
}

func TestNewPasskeyManagerRejectsInvalidConfiguration(t *testing.T) {
	cipher, err := NewSecretCipher(make([]byte, 32))
	if err != nil {
		t.Fatalf("NewSecretCipher() error = %v", err)
	}

	_, err = NewPasskeyManager(
		NewRepository(nil),
		cipher,
		PasskeyConfig{
			RPID:          "",
			RPDisplayName: "Anamnesys",
			RPOrigins:     nil,
			CeremonyTTL:   5 * time.Minute,
		},
	)
	if err == nil {
		t.Fatal("NewPasskeyManager() error = nil, want invalid configuration error")
	}
}

func TestGenerateRecoveryCodes(t *testing.T) {
	codes, hashes, err := generateRecoveryCodes(10)
	if err != nil {
		t.Fatalf("generateRecoveryCodes() error = %v", err)
	}
	if len(codes) != 10 || len(hashes) != 10 {
		t.Fatalf("got %d codes and %d hashes, want 10 of each", len(codes), len(hashes))
	}

	seen := make(map[string]struct{}, len(codes))
	for index, code := range codes {
		if len(code) != 17 || code[8] != '-' {
			t.Fatalf("code %q does not use the expected 8-8 format", code)
		}
		if hashes[index] != hashToken(normalizeRecoveryCode(code)) {
			t.Fatalf("hash %d does not match its normalized recovery code", index)
		}
		if _, exists := seen[code]; exists {
			t.Fatalf("duplicate recovery code generated: %q", code)
		}
		seen[code] = struct{}{}
	}
}
