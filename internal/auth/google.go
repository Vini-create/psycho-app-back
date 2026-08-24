package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/api/idtoken"
)

type GoogleIdentity struct {
	Subject            string
	Email              string
	DisplayName        string
	Nonce              string
	AuthoritativeEmail bool
}

type GoogleTokenVerifier interface {
	Verify(context.Context, string) (GoogleIdentity, error)
}

type GoogleIDTokenVerifier struct {
	clientID string
}

func NewGoogleIDTokenVerifier(clientID string) (*GoogleIDTokenVerifier, error) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return nil, fmt.Errorf("Google client ID is required")
	}
	return &GoogleIDTokenVerifier{clientID: clientID}, nil
}

func (v *GoogleIDTokenVerifier) Verify(ctx context.Context, credential string) (GoogleIdentity, error) {
	payload, err := idtoken.Validate(ctx, credential, v.clientID)
	if err != nil {
		return GoogleIdentity{}, ErrInvalidGoogleCredential
	}
	return googleIdentityFromPayload(payload)
}

func googleIdentityFromPayload(payload *idtoken.Payload) (GoogleIdentity, error) {
	if payload == nil {
		return GoogleIdentity{}, ErrInvalidGoogleCredential
	}
	if payload.Issuer != "accounts.google.com" && payload.Issuer != "https://accounts.google.com" {
		return GoogleIdentity{}, ErrInvalidGoogleCredential
	}
	email, _ := payload.Claims["email"].(string)
	displayName, _ := payload.Claims["name"].(string)
	nonce, _ := payload.Claims["nonce"].(string)
	hostedDomain, _ := payload.Claims["hd"].(string)
	emailVerified, _ := payload.Claims["email_verified"].(bool)
	email = strings.ToLower(strings.TrimSpace(email))
	if payload.Subject == "" || email == "" || nonce == "" || !emailVerified {
		return GoogleIdentity{}, ErrInvalidGoogleCredential
	}
	return GoogleIdentity{
		Subject: payload.Subject, Email: email, DisplayName: strings.TrimSpace(displayName),
		Nonce:              nonce,
		AuthoritativeEmail: strings.HasSuffix(email, "@gmail.com") || hostedDomain != "",
	}, nil
}

type DisabledGoogleVerifier struct{}

func (DisabledGoogleVerifier) Verify(context.Context, string) (GoogleIdentity, error) {
	return GoogleIdentity{}, ErrGoogleNotConfigured
}

type GoogleChallenge struct {
	ChallengeID string    `json:"challenge_id"`
	Nonce       string    `json:"nonce"`
	ExpiresAt   time.Time `json:"expires_at"`
}

func (s *Service) BeginGoogleLogin(
	ctx context.Context,
	audience Audience,
	client ClientInfo,
) (GoogleChallenge, error) {
	if !audience.Valid() {
		return GoogleChallenge{}, ErrInvalidInput
	}
	if _, disabled := s.google.(DisabledGoogleVerifier); disabled {
		return GoogleChallenge{}, ErrGoogleNotConfigured
	}
	nonce, err := randomToken(32)
	if err != nil {
		return GoogleChallenge{}, err
	}
	expiresAt := s.now().UTC().Add(s.config.GoogleChallengeTTL)
	challengeID, err := s.repository.CreateGoogleChallenge(
		ctx, audience, hashToken(nonce), expiresAt, client,
	)
	if err != nil {
		return GoogleChallenge{}, err
	}
	return GoogleChallenge{ChallengeID: challengeID, Nonce: nonce, ExpiresAt: expiresAt}, nil
}

func (s *Service) LoginWithGoogle(
	ctx context.Context,
	audience Audience,
	challengeID string,
	credential string,
	client ClientInfo,
) (LoginResult, error) {
	if !audience.Valid() || challengeID == "" || len(credential) < 100 || len(credential) > 20_000 {
		return LoginResult{}, ErrInvalidInput
	}
	identity, err := s.google.Verify(ctx, credential)
	if err != nil {
		s.recordEvent(ctx, Event{Audience: audience, Type: "google_login", Outcome: "failure", Client: client})
		return LoginResult{}, err
	}
	if err := s.repository.ConsumeGoogleChallenge(
		ctx, audience, challengeID, hashToken(identity.Nonce), s.now().UTC(),
	); err != nil {
		return LoginResult{}, err
	}
	if _, err := normalizeAndValidateEmail(identity.Email); err != nil {
		return LoginResult{}, ErrInvalidGoogleCredential
	}
	displayName := identity.DisplayName
	if displayName == "" {
		displayName = strings.SplitN(identity.Email, "@", 2)[0]
	}
	displayName, err = normalizeDisplayName(displayName)
	if err != nil {
		return LoginResult{}, ErrInvalidGoogleCredential
	}
	randomPassword, err := randomToken(32)
	if err != nil {
		return LoginResult{}, err
	}
	passwordHash, err := s.passwords.Hash(randomPassword)
	if err != nil {
		return LoginResult{}, fmt.Errorf("hash Google account password: %w", err)
	}
	account, _, err := s.repository.FindOrCreateGoogleAccount(
		ctx, audience, identity, displayName, passwordHash, s.now().UTC(),
	)
	if err != nil {
		if errors.Is(err, ErrGoogleLinkRequired) {
			return LoginResult{}, err
		}
		return LoginResult{}, err
	}
	return s.completePrimaryLogin(ctx, audience, account, "google_login", client)
}
