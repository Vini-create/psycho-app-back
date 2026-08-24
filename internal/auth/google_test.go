package auth

import (
	"errors"
	"testing"

	"google.golang.org/api/idtoken"
)

func TestGoogleIdentityFromPayload(t *testing.T) {
	t.Parallel()
	identity, err := googleIdentityFromPayload(&idtoken.Payload{
		Issuer:  "https://accounts.google.com",
		Subject: "google-subject-123",
		Claims: map[string]any{
			"email": "Person@Gmail.com", "email_verified": true,
			"name": "Person Name", "nonce": "one-time-nonce",
		},
	})
	if err != nil {
		t.Fatalf("googleIdentityFromPayload() error = %v", err)
	}
	if identity.Subject != "google-subject-123" || identity.Email != "person@gmail.com" ||
		identity.Nonce != "one-time-nonce" || !identity.AuthoritativeEmail {
		t.Fatalf("unexpected identity: %+v", identity)
	}
}

func TestGoogleIdentityFromPayloadRejectsWrongIssuer(t *testing.T) {
	t.Parallel()
	_, err := googleIdentityFromPayload(&idtoken.Payload{
		Issuer: "https://attacker.example.com", Subject: "subject",
		Claims: map[string]any{
			"email": "person@gmail.com", "email_verified": true, "nonce": "nonce",
		},
	})
	if !errors.Is(err, ErrInvalidGoogleCredential) {
		t.Fatalf("error = %v", err)
	}
}

func TestGoogleIdentityFromPayloadRequiresVerifiedEmailAndNonce(t *testing.T) {
	t.Parallel()
	for _, claims := range []map[string]any{
		{"email": "person@gmail.com", "email_verified": false, "nonce": "nonce"},
		{"email": "person@gmail.com", "email_verified": true},
	} {
		_, err := googleIdentityFromPayload(&idtoken.Payload{
			Issuer: "accounts.google.com", Subject: "subject", Claims: claims,
		})
		if !errors.Is(err, ErrInvalidGoogleCredential) {
			t.Fatalf("claims %+v error = %v", claims, err)
		}
	}
}
