package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
)

const (
	webAuthnPurposeRegistration   = "registration"
	webAuthnPurposeAuthentication = "authentication"
)

type PasskeyConfig struct {
	RPID          string
	RPDisplayName string
	RPOrigins     []string
	CeremonyTTL   time.Duration
}

type PasskeyManager struct {
	repository  *Repository
	cipher      *SecretCipher
	webAuthn    *webauthn.WebAuthn
	ceremonyTTL time.Duration
	now         func() time.Time
}

type passkeyUser struct {
	id          []byte
	email       string
	displayName string
	credentials []webauthn.Credential
}

func (u passkeyUser) WebAuthnID() []byte {
	return u.id
}

func (u passkeyUser) WebAuthnName() string {
	return u.email
}

func (u passkeyUser) WebAuthnDisplayName() string {
	return u.displayName
}

func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential {
	return u.credentials
}

type PasskeyCeremony struct {
	CeremonyToken string `json:"ceremony_token"`
	PublicKey     any    `json:"public_key"`
}

type PasskeyRegistrationResult struct {
	Passkey       PasskeyInfo `json:"passkey"`
	RecoveryCodes []string    `json:"recovery_codes,omitempty"`
	AccessToken   string      `json:"access_token"`
	TokenType     string      `json:"token_type"`
	ExpiresAt     time.Time   `json:"expires_at"`
}

type PasskeyInfo struct {
	ID         string     `json:"id"`
	Label      *string    `json:"label,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

func NewPasskeyManager(
	repository *Repository,
	cipher *SecretCipher,
	config PasskeyConfig,
) (*PasskeyManager, error) {
	if repository == nil || cipher == nil {
		return nil, fmt.Errorf("passkey manager dependencies are required")
	}
	if config.CeremonyTTL <= 0 {
		return nil, fmt.Errorf("WebAuthn ceremony TTL must be greater than zero")
	}

	webAuthn, err := webauthn.New(&webauthn.Config{
		RPID:                  config.RPID,
		RPDisplayName:         config.RPDisplayName,
		RPOrigins:             config.RPOrigins,
		RPAllowCrossOrigin:    false,
		AttestationPreference: protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementPreferred,
			UserVerification: protocol.VerificationRequired,
		},
		Timeouts: webauthn.TimeoutsConfig{
			Login: webauthn.TimeoutConfig{
				Enforce: true,
				Timeout: config.CeremonyTTL,
			},
			Registration: webauthn.TimeoutConfig{
				Enforce: true,
				Timeout: config.CeremonyTTL,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create WebAuthn relying party: %w", err)
	}

	return &PasskeyManager{
		repository:  repository,
		cipher:      cipher,
		webAuthn:    webAuthn,
		ceremonyTTL: config.CeremonyTTL,
		now:         time.Now,
	}, nil
}

func (m *PasskeyManager) HasPasskeys(ctx context.Context, professionalUserID string) (bool, error) {
	return m.repository.HasActivePasskeys(ctx, professionalUserID)
}

func (m *PasskeyManager) BeginRegistration(
	ctx context.Context,
	professionalUserID string,
	client ClientInfo,
) (PasskeyCeremony, error) {
	user, _, err := m.loadUser(ctx, professionalUserID)
	if err != nil {
		return PasskeyCeremony{}, err
	}

	creation, session, err := m.webAuthn.BeginRegistration(
		user,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementPreferred),
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementPreferred,
			UserVerification: protocol.VerificationRequired,
		}),
		webauthn.WithConveyancePreference(protocol.PreferNoAttestation),
	)
	if err != nil {
		return PasskeyCeremony{}, fmt.Errorf("begin passkey registration: %w", err)
	}

	ceremonyToken, err := m.storeCeremony(
		ctx,
		professionalUserID,
		webAuthnPurposeRegistration,
		session,
		client,
	)
	if err != nil {
		return PasskeyCeremony{}, err
	}

	return PasskeyCeremony{
		CeremonyToken: ceremonyToken,
		PublicKey:     creation.Response,
	}, nil
}

func (m *PasskeyManager) FinishRegistration(
	ctx context.Context,
	professionalUserID string,
	sessionID string,
	ceremonyToken string,
	credentialResponse json.RawMessage,
	label string,
	client ClientInfo,
) (PasskeyRegistrationResult, error) {
	label = strings.TrimSpace(label)
	if len([]rune(label)) > 100 {
		return PasskeyRegistrationResult{}, ErrInvalidInput
	}
	if ceremonyToken == "" || len(credentialResponse) == 0 {
		return PasskeyRegistrationResult{}, ErrInvalidInput
	}

	now := m.now().UTC()
	ceremony, err := m.repository.GetWebAuthnCeremony(
		ctx,
		professionalUserID,
		webAuthnPurposeRegistration,
		hashToken(ceremonyToken),
		now,
	)
	if err != nil {
		return PasskeyRegistrationResult{}, err
	}

	session, err := m.decryptSession(ceremony.SessionDataCiphertext)
	if err != nil {
		return PasskeyRegistrationResult{}, err
	}
	user, _, err := m.loadUser(ctx, professionalUserID)
	if err != nil {
		return PasskeyRegistrationResult{}, err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(credentialResponse)
	if err != nil {
		return PasskeyRegistrationResult{}, ErrInvalidToken
	}
	credential, err := m.webAuthn.CreateCredential(user, session, parsed)
	if err != nil {
		return PasskeyRegistrationResult{}, ErrInvalidToken
	}

	credentialCiphertext, err := m.encryptCredential(*credential)
	if err != nil {
		return PasskeyRegistrationResult{}, err
	}
	recoveryCodes, recoveryCodeHashes, err := generateRecoveryCodes(10)
	if err != nil {
		return PasskeyRegistrationResult{}, err
	}
	created, recoveryCodesCreated, err := m.repository.CompletePasskeyRegistration(
		ctx,
		ceremony.ID,
		professionalUserID,
		sessionID,
		credential.ID,
		credentialCiphertext,
		label,
		recoveryCodeHashes,
		client,
		now,
	)
	if err != nil {
		return PasskeyRegistrationResult{}, err
	}
	if !recoveryCodesCreated {
		recoveryCodes = nil
	}

	return PasskeyRegistrationResult{
		Passkey: PasskeyInfo{
			ID:         created.ID,
			Label:      created.Label,
			CreatedAt:  created.CreatedAt,
			LastUsedAt: created.LastUsedAt,
		},
		RecoveryCodes: recoveryCodes,
	}, nil
}

func (m *PasskeyManager) BeginAuthentication(
	ctx context.Context,
	professionalUserID string,
	client ClientInfo,
) (PasskeyCeremony, error) {
	user, _, err := m.loadUser(ctx, professionalUserID)
	if err != nil {
		return PasskeyCeremony{}, err
	}
	if len(user.credentials) == 0 {
		return PasskeyCeremony{}, ErrNotFound
	}

	assertion, session, err := m.webAuthn.BeginLogin(
		user,
		webauthn.WithUserVerification(protocol.VerificationRequired),
		webauthn.WithAssertionPublicKeyCredentialHints([]protocol.PublicKeyCredentialHints{
			protocol.PublicKeyCredentialHintClientDevice,
			protocol.PublicKeyCredentialHintHybrid,
			protocol.PublicKeyCredentialHintSecurityKey,
		}),
	)
	if err != nil {
		return PasskeyCeremony{}, fmt.Errorf("begin passkey authentication: %w", err)
	}

	ceremonyToken, err := m.storeCeremony(
		ctx,
		professionalUserID,
		webAuthnPurposeAuthentication,
		session,
		client,
	)
	if err != nil {
		return PasskeyCeremony{}, err
	}

	return PasskeyCeremony{
		CeremonyToken: ceremonyToken,
		PublicKey:     assertion.Response,
	}, nil
}

func (m *PasskeyManager) FinishAuthentication(
	ctx context.Context,
	ceremonyToken string,
	credentialResponse json.RawMessage,
	client ClientInfo,
) (string, error) {
	if ceremonyToken == "" || len(credentialResponse) == 0 {
		return "", ErrInvalidInput
	}

	now := m.now().UTC()
	ceremony, err := m.repository.GetWebAuthnAuthenticationCeremony(
		ctx,
		hashToken(ceremonyToken),
		now,
	)
	if err != nil {
		return "", err
	}

	session, err := m.decryptSession(ceremony.SessionDataCiphertext)
	if err != nil {
		return "", err
	}
	user, _, err := m.loadUser(ctx, ceremony.ProfessionalUserID)
	if err != nil {
		return "", err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(credentialResponse)
	if err != nil {
		return "", ErrInvalidToken
	}
	credential, err := m.webAuthn.ValidateLogin(user, session, parsed)
	if err != nil {
		return "", ErrInvalidToken
	}

	credentialCiphertext, err := m.encryptCredential(*credential)
	if err != nil {
		return "", err
	}
	if err := m.repository.CompletePasskeyAuthentication(
		ctx,
		ceremony.ID,
		ceremony.ProfessionalUserID,
		credential.ID,
		credentialCiphertext,
		client,
		now,
	); err != nil {
		return "", err
	}

	return ceremony.ProfessionalUserID, nil
}

func (m *PasskeyManager) RecoverAuthentication(
	ctx context.Context,
	ceremonyToken string,
	recoveryCode string,
) (string, error) {
	if ceremonyToken == "" || recoveryCode == "" {
		return "", ErrInvalidInput
	}

	now := m.now().UTC()
	ceremony, err := m.repository.GetWebAuthnAuthenticationCeremony(
		ctx,
		hashToken(ceremonyToken),
		now,
	)
	if err != nil {
		return "", err
	}
	if err := m.repository.UseProfessionalRecoveryCode(
		ctx,
		ceremony.ID,
		ceremony.ProfessionalUserID,
		hashToken(normalizeRecoveryCode(recoveryCode)),
		now,
	); err != nil {
		return "", err
	}

	return ceremony.ProfessionalUserID, nil
}

func (m *PasskeyManager) List(
	ctx context.Context,
	professionalUserID string,
) ([]PasskeyInfo, error) {
	stored, err := m.repository.ListStoredPasskeyCredentials(ctx, professionalUserID)
	if err != nil {
		return nil, err
	}
	passkeys := make([]PasskeyInfo, 0, len(stored))
	for _, credential := range stored {
		passkeys = append(passkeys, PasskeyInfo{
			ID:         credential.ID,
			Label:      credential.Label,
			CreatedAt:  credential.CreatedAt,
			LastUsedAt: credential.LastUsedAt,
		})
	}
	return passkeys, nil
}

func (m *PasskeyManager) Remove(
	ctx context.Context,
	professionalUserID string,
	passkeyID string,
) error {
	if passkeyID == "" {
		return ErrInvalidInput
	}
	return m.repository.RevokePasskey(ctx, professionalUserID, passkeyID, m.now().UTC())
}

func (m *PasskeyManager) RegenerateRecoveryCodes(
	ctx context.Context,
	professionalUserID string,
) ([]string, error) {
	hasPasskeys, err := m.repository.HasActivePasskeys(ctx, professionalUserID)
	if err != nil {
		return nil, err
	}
	if !hasPasskeys {
		return nil, ErrNotFound
	}

	codes, hashes, err := generateRecoveryCodes(10)
	if err != nil {
		return nil, err
	}
	if err := m.repository.ReplaceProfessionalRecoveryCodes(
		ctx,
		professionalUserID,
		hashes,
		m.now().UTC(),
	); err != nil {
		return nil, err
	}
	return codes, nil
}

func (m *PasskeyManager) loadUser(
	ctx context.Context,
	professionalUserID string,
) (passkeyUser, []storedPasskeyCredential, error) {
	account, err := m.repository.FindAccountByID(ctx, AudienceProfessional, professionalUserID)
	if err != nil {
		return passkeyUser{}, nil, err
	}
	parsedID, err := uuid.Parse(account.ID)
	if err != nil {
		return passkeyUser{}, nil, fmt.Errorf("parse professional WebAuthn ID: %w", err)
	}
	stored, err := m.repository.ListStoredPasskeyCredentials(ctx, professionalUserID)
	if err != nil {
		return passkeyUser{}, nil, err
	}

	credentials := make([]webauthn.Credential, 0, len(stored))
	for _, record := range stored {
		plaintext, err := m.cipher.Decrypt(record.CredentialCiphertext)
		if err != nil {
			return passkeyUser{}, nil, fmt.Errorf("decrypt passkey credential: %w", err)
		}
		var credential webauthn.Credential
		if err := json.Unmarshal(plaintext, &credential); err != nil {
			return passkeyUser{}, nil, fmt.Errorf("decode passkey credential: %w", err)
		}
		credentials = append(credentials, credential)
	}

	return passkeyUser{
		id:          parsedID[:],
		email:       account.Email,
		displayName: account.DisplayName,
		credentials: credentials,
	}, stored, nil
}

func (m *PasskeyManager) storeCeremony(
	ctx context.Context,
	professionalUserID string,
	purpose string,
	session *webauthn.SessionData,
	client ClientInfo,
) (string, error) {
	encodedSession, err := json.Marshal(session)
	if err != nil {
		return "", fmt.Errorf("encode WebAuthn session: %w", err)
	}
	encryptedSession, err := m.cipher.Encrypt(encodedSession)
	if err != nil {
		return "", err
	}
	ceremonyToken, err := randomToken(32)
	if err != nil {
		return "", err
	}

	now := m.now().UTC()
	expiresAt := now.Add(m.ceremonyTTL)
	if session.Expires.Before(expiresAt) {
		expiresAt = session.Expires
	}
	if err := m.repository.CreateWebAuthnCeremony(
		ctx,
		professionalUserID,
		purpose,
		hashToken(ceremonyToken),
		encryptedSession,
		expiresAt,
		client,
		now,
	); err != nil {
		return "", err
	}

	return ceremonyToken, nil
}

func (m *PasskeyManager) decryptSession(ciphertext []byte) (webauthn.SessionData, error) {
	plaintext, err := m.cipher.Decrypt(ciphertext)
	if err != nil {
		return webauthn.SessionData{}, err
	}
	var session webauthn.SessionData
	if err := json.Unmarshal(plaintext, &session); err != nil {
		return webauthn.SessionData{}, fmt.Errorf("decode WebAuthn session: %w", err)
	}
	return session, nil
}

func (m *PasskeyManager) encryptCredential(credential webauthn.Credential) ([]byte, error) {
	encoded, err := json.Marshal(credential)
	if err != nil {
		return nil, fmt.Errorf("encode passkey credential: %w", err)
	}
	return m.cipher.Encrypt(encoded)
}
