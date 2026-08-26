package auth

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

type ServiceConfig struct {
	RefreshTokenTTL           time.Duration
	EmailVerificationTokenTTL time.Duration
	PasswordResetTokenTTL     time.Duration
	GoogleChallengeTTL        time.Duration
	ExposeDevelopmentTokens   bool
}

type Service struct {
	repository        *Repository
	passwords         PasswordHasher
	accessTokens      *AccessTokenManager
	passkeys          *PasskeyManager
	cipher            *SecretCipher
	google            GoogleTokenVerifier
	config            ServiceConfig
	dummyPasswordHash string
	now               func() time.Time
}

func NewService(
	repository *Repository,
	accessTokens *AccessTokenManager,
	passkeys *PasskeyManager,
	cipher *SecretCipher,
	google GoogleTokenVerifier,
	config ServiceConfig,
) (*Service, error) {
	if repository == nil || accessTokens == nil || passkeys == nil || cipher == nil || google == nil {
		return nil, fmt.Errorf("auth service dependencies are required")
	}
	if config.RefreshTokenTTL <= 0 ||
		config.EmailVerificationTokenTTL <= 0 ||
		config.PasswordResetTokenTTL <= 0 ||
		config.GoogleChallengeTTL <= 0 {
		return nil, fmt.Errorf("auth token TTLs must be greater than zero")
	}

	passwords := PasswordHasher{}
	dummyPasswordHash, err := passwords.Hash("dummy-password-never-authenticates")
	if err != nil {
		return nil, fmt.Errorf("create dummy password hash: %w", err)
	}

	return &Service{
		repository:        repository,
		passwords:         passwords,
		accessTokens:      accessTokens,
		passkeys:          passkeys,
		cipher:            cipher,
		google:            google,
		config:            config,
		dummyPasswordHash: dummyPasswordHash,
		now:               time.Now,
	}, nil
}

func (s *Service) Register(
	ctx context.Context,
	audience Audience,
	input RegisterInput,
	client ClientInfo,
) (RegisterResult, error) {
	if !audience.Valid() {
		return RegisterResult{}, ErrInvalidInput
	}

	email, err := normalizeAndValidateEmail(input.Email)
	if err != nil {
		return RegisterResult{}, err
	}
	displayName, err := normalizeDisplayName(input.DisplayName)
	if err != nil {
		return RegisterResult{}, err
	}
	if err := ValidatePassword(input.Password); err != nil {
		return RegisterResult{}, err
	}

	passwordHash, err := s.passwords.Hash(input.Password)
	if err != nil {
		return RegisterResult{}, fmt.Errorf("hash password: %w", err)
	}

	rawVerificationToken, err := randomToken(32)
	if err != nil {
		return RegisterResult{}, err
	}

	now := s.now().UTC()
	tokenCiphertext, err := s.cipher.Encrypt([]byte(rawVerificationToken))
	if err != nil {
		return RegisterResult{}, fmt.Errorf("encrypt verification token: %w", err)
	}
	accountID, err := s.repository.CreateAccount(
		ctx,
		audience,
		email,
		passwordHash,
		displayName,
		hashToken(rawVerificationToken),
		now.Add(s.config.EmailVerificationTokenTTL),
		EmailOutboxMessage{
			Kind: "email_verification", RecipientEmail: email,
			TokenCiphertext: tokenCiphertext,
		},
		client,
	)
	if err != nil {
		return RegisterResult{}, err
	}

	s.recordEvent(ctx, Event{
		Audience:  audience,
		AccountID: accountID,
		Type:      "register",
		Outcome:   "success",
		Client:    client,
	})

	result := RegisterResult{
		AccountID:            accountID,
		Email:                email,
		VerificationRequired: true,
	}
	if s.config.ExposeDevelopmentTokens {
		result.DevelopmentToken = rawVerificationToken
	}

	return result, nil
}

func (s *Service) Login(
	ctx context.Context,
	audience Audience,
	emailInput string,
	password string,
	client ClientInfo,
) (LoginResult, error) {
	email, err := normalizeAndValidateEmail(emailInput)
	if err != nil {
		s.compareDummyPassword(password)
		return LoginResult{}, ErrInvalidCredentials
	}

	account, err := s.repository.FindAccountByEmail(ctx, audience, email)
	if errors.Is(err, ErrNotFound) {
		s.compareDummyPassword(password)
		s.recordEvent(ctx, Event{Audience: audience, Type: "login", Outcome: "failure", Client: client})
		return LoginResult{}, ErrInvalidCredentials
	}
	if err != nil {
		return LoginResult{}, err
	}

	passwordMatches, err := s.passwords.Compare(password, account.PasswordHash)
	if err != nil {
		return LoginResult{}, fmt.Errorf("compare password: %w", err)
	}
	if !passwordMatches {
		s.recordEvent(ctx, Event{Audience: audience, AccountID: account.ID, Type: "login", Outcome: "failure", Client: client})
		return LoginResult{}, ErrInvalidCredentials
	}

	return s.completePrimaryLogin(ctx, audience, account, "login", client)
}

func (s *Service) completePrimaryLogin(
	ctx context.Context,
	audience Audience,
	account Account,
	eventType string,
	client ClientInfo,
) (LoginResult, error) {
	if account.Status == "pending_verification" || account.EmailVerifiedAt == nil {
		return LoginResult{}, ErrEmailNotVerified
	}
	if account.Status != "active" {
		return LoginResult{}, ErrAccountUnavailable
	}

	if audience == AudienceProfessional {
		hasPasskeys, err := s.passkeys.HasPasskeys(ctx, account.ID)
		if err != nil {
			return LoginResult{}, err
		}
		if hasPasskeys {
			ceremony, err := s.passkeys.BeginAuthentication(ctx, account.ID, client)
			if err != nil {
				return LoginResult{}, err
			}
			return LoginResult{PasskeyRequired: true, PasskeyCeremony: &ceremony}, nil
		}
	}

	tokens, err := s.issueSession(ctx, audience, account.ID, false, client)
	if err != nil {
		return LoginResult{}, err
	}
	s.recordEvent(ctx, Event{
		Audience: audience, AccountID: account.ID, Type: eventType,
		Outcome: "success", Client: client,
	})
	return LoginResult{
		Tokens:                  &tokens,
		PasskeyEnrollmentNeeded: audience == AudienceProfessional,
	}, nil
}

func (s *Service) Refresh(
	ctx context.Context,
	audience Audience,
	rawRefreshToken string,
	client ClientInfo,
) (TokenPair, error) {
	if rawRefreshToken == "" {
		return TokenPair{}, ErrInvalidRefreshToken
	}

	newRawRefreshToken, err := randomToken(32)
	if err != nil {
		return TokenPair{}, err
	}

	now := s.now().UTC()
	session, err := s.repository.RotateSession(
		ctx,
		audience,
		hashToken(rawRefreshToken),
		hashToken(newRawRefreshToken),
		now.Add(s.config.RefreshTokenTTL),
		client,
		now,
	)
	if err != nil {
		return TokenPair{}, err
	}

	principal := Principal{
		AccountID: session.AccountID,
		SessionID: session.ID,
		Audience:  session.Audience,
		MFA:       session.MFA,
	}
	accessToken, accessExpiresAt, err := s.accessTokens.Issue(principal)
	if err != nil {
		return TokenPair{}, err
	}

	s.recordEvent(ctx, Event{
		Audience:  session.Audience,
		AccountID: session.AccountID,
		SessionID: session.ID,
		Type:      "refresh",
		Outcome:   "success",
		Client:    client,
	})

	return TokenPair{
		AccessToken:  accessToken,
		RefreshToken: newRawRefreshToken,
		TokenType:    "Bearer",
		ExpiresAt:    accessExpiresAt,
	}, nil
}

func (s *Service) Authenticate(
	ctx context.Context,
	audience Audience,
	rawAccessToken string,
) (Principal, error) {
	principal, err := s.accessTokens.Parse(rawAccessToken, audience)
	if err != nil {
		return Principal{}, ErrInvalidToken
	}

	active, err := s.repository.SessionIsActive(ctx, principal, s.now().UTC())
	if err != nil {
		return Principal{}, err
	}
	if !active {
		return Principal{}, ErrInvalidToken
	}

	return principal, nil
}

func (s *Service) RevokeSession(
	ctx context.Context,
	principal Principal,
	sessionID string,
	client ClientInfo,
) error {
	revoked, err := s.repository.RevokeSessionFamily(
		ctx,
		principal,
		sessionID,
		"user_logout",
		s.now().UTC(),
	)
	if err != nil {
		return err
	}
	if !revoked {
		return ErrNotFound
	}

	s.recordEvent(ctx, Event{
		Audience:  principal.Audience,
		AccountID: principal.AccountID,
		SessionID: sessionID,
		Type:      "session_revoked",
		Outcome:   "success",
		Client:    client,
	})
	return nil
}

func (s *Service) LogoutAll(
	ctx context.Context,
	principal Principal,
	client ClientInfo,
) error {
	if err := s.repository.RevokeAllSessions(
		ctx,
		principal,
		"user_logout_all",
		s.now().UTC(),
	); err != nil {
		return err
	}

	s.recordEvent(ctx, Event{
		Audience:  principal.Audience,
		AccountID: principal.AccountID,
		SessionID: principal.SessionID,
		Type:      "logout_all",
		Outcome:   "success",
		Client:    client,
	})
	return nil
}

func (s *Service) ListSessions(ctx context.Context, principal Principal) ([]Session, error) {
	return s.repository.ListSessions(ctx, principal, s.now().UTC())
}

func (s *Service) RequestEmailVerification(
	ctx context.Context,
	audience Audience,
	emailInput string,
	client ClientInfo,
) (OneTimeTokenResult, error) {
	return s.requestOneTimeToken(
		ctx,
		audience,
		emailInput,
		"email_verification",
		s.config.EmailVerificationTokenTTL,
		"email_verification_requested",
		client,
	)
}

func (s *Service) ConfirmEmail(
	ctx context.Context,
	audience Audience,
	rawToken string,
	client ClientInfo,
) error {
	if rawToken == "" {
		return ErrInvalidToken
	}
	if err := s.repository.VerifyEmail(ctx, audience, hashToken(rawToken), s.now().UTC()); err != nil {
		return err
	}
	s.recordEvent(ctx, Event{Audience: audience, Type: "email_verified", Outcome: "success", Client: client})
	return nil
}

func (s *Service) RequestPasswordReset(
	ctx context.Context,
	audience Audience,
	emailInput string,
	client ClientInfo,
) (OneTimeTokenResult, error) {
	return s.requestOneTimeToken(
		ctx,
		audience,
		emailInput,
		"password_reset",
		s.config.PasswordResetTokenTTL,
		"password_reset_requested",
		client,
	)
}

func (s *Service) CompletePasswordReset(
	ctx context.Context,
	audience Audience,
	rawToken string,
	newPassword string,
	client ClientInfo,
) error {
	if rawToken == "" {
		return ErrInvalidToken
	}
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	passwordHash, err := s.passwords.Hash(newPassword)
	if err != nil {
		return fmt.Errorf("hash reset password: %w", err)
	}
	if err := s.repository.ResetPassword(
		ctx,
		audience,
		hashToken(rawToken),
		passwordHash,
		s.now().UTC(),
	); err != nil {
		return err
	}
	s.recordEvent(ctx, Event{Audience: audience, Type: "password_reset_completed", Outcome: "success", Client: client})
	return nil
}

func (s *Service) BeginPasskeyRegistration(
	ctx context.Context,
	principal Principal,
	client ClientInfo,
) (PasskeyCeremony, error) {
	if principal.Audience != AudienceProfessional {
		return PasskeyCeremony{}, ErrForbidden
	}
	hasPasskeys, err := s.passkeys.HasPasskeys(ctx, principal.AccountID)
	if err != nil {
		return PasskeyCeremony{}, err
	}
	if hasPasskeys && !principal.MFA {
		return PasskeyCeremony{}, ErrForbidden
	}

	ceremony, err := s.passkeys.BeginRegistration(ctx, principal.AccountID, client)
	if err != nil {
		return PasskeyCeremony{}, err
	}
	s.recordEvent(ctx, Event{
		Audience: AudienceProfessional, AccountID: principal.AccountID,
		SessionID: principal.SessionID, Type: "passkey_registration_started",
		Outcome: "success", Client: client,
	})
	return ceremony, nil
}

func (s *Service) FinishPasskeyRegistration(
	ctx context.Context,
	principal Principal,
	ceremonyToken string,
	credentialResponse []byte,
	label string,
	client ClientInfo,
) (PasskeyRegistrationResult, error) {
	if principal.Audience != AudienceProfessional {
		return PasskeyRegistrationResult{}, ErrForbidden
	}

	result, err := s.passkeys.FinishRegistration(
		ctx,
		principal.AccountID,
		principal.SessionID,
		ceremonyToken,
		credentialResponse,
		label,
		client,
	)
	if err != nil {
		return PasskeyRegistrationResult{}, err
	}
	principal.MFA = true
	accessToken, expiresAt, err := s.accessTokens.Issue(principal)
	if err != nil {
		return PasskeyRegistrationResult{}, err
	}

	result.AccessToken = accessToken
	result.TokenType = "Bearer"
	result.ExpiresAt = expiresAt
	s.recordEvent(ctx, Event{
		Audience: AudienceProfessional, AccountID: principal.AccountID,
		SessionID: principal.SessionID, Type: "passkey_registered",
		Outcome: "success", Client: client,
	})
	return result, nil
}

func (s *Service) FinishPasskeyAuthentication(
	ctx context.Context,
	ceremonyToken string,
	credentialResponse []byte,
	client ClientInfo,
) (TokenPair, error) {
	professionalUserID, err := s.passkeys.FinishAuthentication(
		ctx, ceremonyToken, credentialResponse, client,
	)
	if err != nil {
		s.recordEvent(ctx, Event{
			Audience: AudienceProfessional, Type: "passkey_authentication",
			Outcome: "failure", Client: client,
		})
		return TokenPair{}, err
	}

	tokens, err := s.issueSession(
		ctx,
		AudienceProfessional,
		professionalUserID,
		true,
		client,
	)
	if err != nil {
		return TokenPair{}, err
	}
	s.recordEvent(ctx, Event{
		Audience: AudienceProfessional, AccountID: professionalUserID,
		Type: "passkey_authentication", Outcome: "success", Client: client,
	})
	s.recordEvent(ctx, Event{
		Audience: AudienceProfessional, AccountID: professionalUserID,
		Type: "login", Outcome: "success", Client: client,
	})
	return tokens, nil
}

func (s *Service) PreviewDeviceAuthorization(
	ctx context.Context,
	scanToken string,
) (DeviceAuthorizationPreview, error) {
	return s.passkeys.PreviewDeviceAuthorization(ctx, scanToken)
}

func (s *Service) ApproveDeviceAuthorization(
	ctx context.Context,
	scanToken string,
	credentialResponse []byte,
	client ClientInfo,
) error {
	professionalUserID, err := s.passkeys.ApproveDeviceAuthorization(
		ctx, scanToken, credentialResponse, client,
	)
	if err != nil {
		s.recordEvent(ctx, Event{
			Audience: AudienceProfessional, Type: "passkey_authentication",
			Outcome: "failure", Client: client,
		})
		return err
	}
	s.recordEvent(ctx, Event{
		Audience: AudienceProfessional, AccountID: professionalUserID,
		Type: "passkey_authentication", Outcome: "success", Client: client,
	})
	return nil
}

func (s *Service) ConsumeDeviceAuthorization(
	ctx context.Context,
	pollToken string,
	client ClientInfo,
) (TokenPair, error) {
	if pollToken == "" {
		return TokenPair{}, ErrInvalidInput
	}
	refreshToken, err := randomToken(32)
	if err != nil {
		return TokenPair{}, err
	}
	now := s.now().UTC()
	sessionID, professionalUserID, err := s.repository.ConsumeDeviceAuthorizationAndCreateSession(
		ctx,
		hashToken(pollToken),
		hashToken(refreshToken),
		now.Add(s.config.RefreshTokenTTL),
		client,
		now,
	)
	if err != nil {
		return TokenPair{}, err
	}
	accessToken, accessExpiresAt, err := s.accessTokens.Issue(Principal{
		AccountID: professionalUserID,
		SessionID: sessionID,
		Audience:  AudienceProfessional,
		MFA:       true,
	})
	if err != nil {
		return TokenPair{}, err
	}
	s.recordEvent(ctx, Event{
		Audience: AudienceProfessional, AccountID: professionalUserID,
		SessionID: sessionID, Type: "login", Outcome: "success", Client: client,
	})
	return TokenPair{
		AccessToken: accessToken, RefreshToken: refreshToken,
		TokenType: "Bearer", ExpiresAt: accessExpiresAt,
	}, nil
}

func (s *Service) RecoverPasskeyAuthentication(
	ctx context.Context,
	ceremonyToken string,
	recoveryCode string,
	client ClientInfo,
) (TokenPair, error) {
	professionalUserID, err := s.passkeys.RecoverAuthentication(
		ctx, ceremonyToken, recoveryCode,
	)
	if err != nil {
		s.recordEvent(ctx, Event{
			Audience: AudienceProfessional, Type: "passkey_recovery_used",
			Outcome: "failure", Client: client,
		})
		return TokenPair{}, err
	}
	tokens, err := s.issueSession(
		ctx, AudienceProfessional, professionalUserID, true, client,
	)
	if err != nil {
		return TokenPair{}, err
	}
	s.recordEvent(ctx, Event{
		Audience: AudienceProfessional, AccountID: professionalUserID,
		Type: "passkey_recovery_used", Outcome: "success", Client: client,
	})
	s.recordEvent(ctx, Event{
		Audience: AudienceProfessional, AccountID: professionalUserID,
		Type: "login", Outcome: "success", Client: client,
	})
	return tokens, nil
}

func (s *Service) ListPasskeys(ctx context.Context, principal Principal) ([]PasskeyInfo, error) {
	if principal.Audience != AudienceProfessional {
		return nil, ErrForbidden
	}
	return s.passkeys.List(ctx, principal.AccountID)
}

func (s *Service) RemovePasskey(
	ctx context.Context,
	principal Principal,
	passkeyID string,
	client ClientInfo,
) error {
	if principal.Audience != AudienceProfessional || !principal.MFA {
		return ErrForbidden
	}
	if err := s.passkeys.Remove(ctx, principal.AccountID, passkeyID); err != nil {
		return err
	}
	s.recordEvent(ctx, Event{
		Audience: AudienceProfessional, AccountID: principal.AccountID,
		SessionID: principal.SessionID, Type: "passkey_removed",
		Outcome: "success", Client: client,
	})
	return nil
}

func (s *Service) RegenerateRecoveryCodes(
	ctx context.Context,
	principal Principal,
	client ClientInfo,
) (RecoveryCodesResult, error) {
	if principal.Audience != AudienceProfessional || !principal.MFA {
		return RecoveryCodesResult{}, ErrForbidden
	}
	codes, err := s.passkeys.RegenerateRecoveryCodes(ctx, principal.AccountID)
	if err != nil {
		return RecoveryCodesResult{}, err
	}
	s.recordEvent(ctx, Event{
		Audience: AudienceProfessional, AccountID: principal.AccountID,
		SessionID: principal.SessionID, Type: "passkey_recovery_codes_regenerated",
		Outcome: "success", Client: client,
	})
	return RecoveryCodesResult{RecoveryCodes: codes}, nil
}

func (s *Service) Account(ctx context.Context, principal Principal) (Account, error) {
	return s.repository.FindAccountByID(ctx, principal.Audience, principal.AccountID)
}

func (s *Service) UpdateDisplayName(
	ctx context.Context,
	principal Principal,
	displayNameInput string,
	client ClientInfo,
) (Account, error) {
	displayName, err := normalizeDisplayName(displayNameInput)
	if err != nil {
		return Account{}, err
	}
	if err := s.repository.UpdateDisplayName(
		ctx, principal.Audience, principal.AccountID, displayName, s.now().UTC(),
	); err != nil {
		return Account{}, err
	}
	s.recordEvent(ctx, Event{
		Audience: principal.Audience, AccountID: principal.AccountID,
		SessionID: principal.SessionID, Type: "profile_updated", Outcome: "success",
		Client: client,
	})
	return s.repository.FindAccountByID(ctx, principal.Audience, principal.AccountID)
}

func (s *Service) ChangePassword(
	ctx context.Context,
	principal Principal,
	currentPassword string,
	newPassword string,
	client ClientInfo,
) error {
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	account, err := s.repository.FindAccountByID(
		ctx, principal.Audience, principal.AccountID,
	)
	if err != nil {
		return err
	}
	matches, err := s.passwords.Compare(currentPassword, account.PasswordHash)
	if err != nil {
		return fmt.Errorf("compare current password: %w", err)
	}
	if !matches {
		return ErrInvalidCredentials
	}
	samePassword, err := s.passwords.Compare(newPassword, account.PasswordHash)
	if err != nil {
		return fmt.Errorf("compare new password: %w", err)
	}
	if samePassword {
		return ErrPasswordUnchanged
	}
	newPasswordHash, err := s.passwords.Hash(newPassword)
	if err != nil {
		return fmt.Errorf("hash changed password: %w", err)
	}
	if err := s.repository.ChangePassword(
		ctx, principal, account.PasswordHash, newPasswordHash, s.now().UTC(),
	); err != nil {
		return err
	}
	s.recordEvent(ctx, Event{
		Audience: principal.Audience, AccountID: principal.AccountID,
		SessionID: principal.SessionID, Type: "password_changed", Outcome: "success",
		Client: client,
	})
	return nil
}

func (s *Service) issueSession(
	ctx context.Context,
	audience Audience,
	accountID string,
	mfa bool,
	client ClientInfo,
) (TokenPair, error) {
	refreshToken, err := randomToken(32)
	if err != nil {
		return TokenPair{}, err
	}

	now := s.now().UTC()
	sessionID, err := s.repository.CreateSession(
		ctx,
		audience,
		accountID,
		hashToken(refreshToken),
		mfa,
		now.Add(s.config.RefreshTokenTTL),
		client,
	)
	if err != nil {
		return TokenPair{}, err
	}

	accessToken, expiresAt, err := s.accessTokens.Issue(Principal{
		AccountID: accountID,
		SessionID: sessionID,
		Audience:  audience,
		MFA:       mfa,
	})
	if err != nil {
		return TokenPair{}, err
	}

	return TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresAt:    expiresAt,
	}, nil
}

func (s *Service) requestOneTimeToken(
	ctx context.Context,
	audience Audience,
	emailInput string,
	purpose string,
	ttl time.Duration,
	eventType string,
	client ClientInfo,
) (OneTimeTokenResult, error) {
	email, err := normalizeAndValidateEmail(emailInput)
	if err != nil {
		return OneTimeTokenResult{}, nil
	}
	rawToken, err := randomToken(32)
	if err != nil {
		return OneTimeTokenResult{}, err
	}
	tokenCiphertext, err := s.cipher.Encrypt([]byte(rawToken))
	if err != nil {
		return OneTimeTokenResult{}, fmt.Errorf("encrypt one-time token: %w", err)
	}
	created, err := s.repository.ReplaceOneTimeTokenByEmail(
		ctx,
		audience,
		email,
		purpose,
		hashToken(rawToken),
		s.now().UTC().Add(ttl),
		EmailOutboxMessage{
			Kind: purpose, RecipientEmail: email, TokenCiphertext: tokenCiphertext,
		},
		client,
	)
	if err != nil {
		return OneTimeTokenResult{}, err
	}

	s.recordEvent(ctx, Event{Audience: audience, Type: eventType, Outcome: "success", Client: client})
	result := OneTimeTokenResult{}
	if created && s.config.ExposeDevelopmentTokens {
		result.DevelopmentToken = rawToken
	}
	return result, nil
}

func generateRecoveryCodes(count int) ([]string, []string, error) {
	codes := make([]string, 0, count)
	hashes := make([]string, 0, count)
	for range count {
		randomBytes := make([]byte, 10)
		if _, err := rand.Read(randomBytes); err != nil {
			return nil, nil, fmt.Errorf("generate recovery code: %w", err)
		}
		normalized := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(randomBytes)
		display := normalized[:8] + "-" + normalized[8:]
		codes = append(codes, display)
		hashes = append(hashes, hashToken(normalized))
	}
	return codes, hashes, nil
}

func normalizeRecoveryCode(code string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
}

func normalizeAndValidateEmail(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if len(normalized) < 3 || len(normalized) > 254 || !utf8.ValidString(normalized) {
		return "", ErrInvalidInput
	}
	parsed, err := mail.ParseAddress(normalized)
	if err != nil || parsed.Address != normalized {
		return "", ErrInvalidInput
	}
	return normalized, nil
}

func normalizeDisplayName(value string) (string, error) {
	normalized := strings.Join(strings.Fields(value), " ")
	length := len([]rune(normalized))
	if length < 1 || length > 120 {
		return "", ErrInvalidInput
	}
	return normalized, nil
}

func (s *Service) compareDummyPassword(password string) {
	_, _ = s.passwords.Compare(password, s.dummyPasswordHash)
}

func (s *Service) recordEvent(ctx context.Context, event Event) {
	if err := s.repository.RecordEvent(ctx, event); err != nil {
		slog.Error("failed to record auth event", "event", event.Type, "error", err)
	}
}
