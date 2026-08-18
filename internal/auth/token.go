package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type AccessTokenManager struct {
	issuer     string
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	ttl        time.Duration
	now        func() time.Time
}

type accessClaims struct {
	SessionID string `json:"sid"`
	MFA       bool   `json:"mfa"`
	jwt.RegisteredClaims
}

func NewAccessTokenManager(issuer string, seed []byte, ttl time.Duration) (*AccessTokenManager, error) {
	if issuer == "" {
		return nil, fmt.Errorf("access token issuer is required")
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("Ed25519 seed must contain %d bytes", ed25519.SeedSize)
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("access token TTL must be greater than zero")
	}

	privateKey := ed25519.NewKeyFromSeed(seed)
	publicKey := privateKey.Public().(ed25519.PublicKey)

	return &AccessTokenManager{
		issuer:     issuer,
		privateKey: privateKey,
		publicKey:  publicKey,
		ttl:        ttl,
		now:        time.Now,
	}, nil
}

func (m *AccessTokenManager) Issue(principal Principal) (string, time.Time, error) {
	now := m.now().UTC()
	expiresAt := now.Add(m.ttl)
	jti, err := randomToken(16)
	if err != nil {
		return "", time.Time{}, err
	}

	claims := accessClaims{
		SessionID: principal.SessionID,
		MFA:       principal.MFA,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   principal.AccountID,
			Audience:  jwt.ClaimStrings{string(principal.Audience)},
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ID:        jti,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	signed, err := token.SignedString(m.privateKey)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}

	return signed, expiresAt, nil
}

func (m *AccessTokenManager) Parse(raw string, audience Audience) (Principal, error) {
	claims := &accessClaims{}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithIssuer(m.issuer),
		jwt.WithAudience(string(audience)),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(30*time.Second),
	)

	token, err := parser.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodEdDSA {
			return nil, ErrInvalidToken
		}
		return m.publicKey, nil
	})
	if err != nil || !token.Valid {
		return Principal{}, ErrInvalidToken
	}
	if claims.Subject == "" || claims.SessionID == "" {
		return Principal{}, ErrInvalidToken
	}

	return Principal{
		AccountID: claims.Subject,
		SessionID: claims.SessionID,
		Audience:  audience,
		MFA:       claims.MFA,
	}, nil
}

func randomToken(byteLength int) (string, error) {
	value := make([]byte, byteLength)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate secure token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func hashToken(raw string) string {
	hash := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(hash[:])
}
