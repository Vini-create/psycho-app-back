package auth

import (
	"errors"
	"testing"
	"time"
)

func TestAccessTokenManagerEnforcesAudience(t *testing.T) {
	seed := make([]byte, 32)
	for index := range seed {
		seed[index] = byte(index + 1)
	}

	manager, err := NewAccessTokenManager("anamnesys-test", seed, 10*time.Minute)
	if err != nil {
		t.Fatalf("NewAccessTokenManager() error = %v", err)
	}

	want := Principal{
		AccountID: "cf1747b9-1a37-4ec8-90e9-3622b642f7f0",
		SessionID: "30b399d3-dc91-4f3e-b1f6-005da3bd0cd6",
		Audience:  AudienceApp,
		MFA:       false,
	}
	raw, _, err := manager.Issue(want)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	got, err := manager.Parse(raw, AudienceApp)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got != want {
		t.Fatalf("Parse() = %#v, want %#v", got, want)
	}

	if _, err := manager.Parse(raw, AudienceProfessional); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Parse() wrong audience error = %v, want ErrInvalidToken", err)
	}
}

func TestOpaqueTokensAreHashedDeterministically(t *testing.T) {
	raw, err := randomToken(32)
	if err != nil {
		t.Fatalf("randomToken() error = %v", err)
	}
	if raw == "" {
		t.Fatal("randomToken() returned an empty token")
	}
	if got := len(hashToken(raw)); got != 64 {
		t.Fatalf("hashToken() length = %d, want 64", got)
	}
}
