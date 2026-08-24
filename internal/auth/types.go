package auth

import "time"

type Audience string

const (
	AudienceApp          Audience = "app"
	AudienceProfessional Audience = "professional"
)

func (a Audience) Valid() bool {
	return a == AudienceApp || a == AudienceProfessional
}

type Account struct {
	ID              string
	Email           string
	PasswordHash    string
	DisplayName     string
	Status          string
	EmailVerifiedAt *time.Time
}

type Principal struct {
	AccountID string
	SessionID string
	Audience  Audience
	MFA       bool
}

type ClientInfo struct {
	IPAddress string
	UserAgent string
}

type TokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"-"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type Session struct {
	ID             string     `json:"id"`
	CreatedIP      *string    `json:"created_ip,omitempty"`
	LastUsedIP     *string    `json:"last_used_ip,omitempty"`
	UserAgent      *string    `json:"user_agent,omitempty"`
	MFA            bool       `json:"mfa_verified"`
	ExpiresAt      time.Time  `json:"expires_at"`
	LastUsedAt     time.Time  `json:"last_used_at"`
	CreatedAt      time.Time  `json:"created_at"`
	RevokedAt      *time.Time `json:"revoked_at,omitempty"`
	CurrentSession bool       `json:"current_session"`
}

type RegisterInput struct {
	Email       string
	Password    string
	DisplayName string
}

type RegisterResult struct {
	AccountID            string `json:"account_id"`
	Email                string `json:"email"`
	VerificationRequired bool   `json:"verification_required"`
	DevelopmentToken     string `json:"development_token,omitempty"`
}

type LoginResult struct {
	Tokens                  *TokenPair       `json:"tokens,omitempty"`
	PasskeyRequired         bool             `json:"passkey_required"`
	PasskeyEnrollmentNeeded bool             `json:"passkey_enrollment_needed,omitempty"`
	PasskeyCeremony         *PasskeyCeremony `json:"passkey_ceremony,omitempty"`
}

type OneTimeTokenResult struct {
	DevelopmentToken string `json:"development_token,omitempty"`
}

type EmailOutboxMessage struct {
	Kind            string
	RecipientEmail  string
	TokenCiphertext []byte
}

type RecoveryCodesResult struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

type Event struct {
	Audience  Audience
	AccountID string
	SessionID string
	Type      string
	Outcome   string
	Client    ClientInfo
}
