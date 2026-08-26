package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type storedDeviceAuthorization struct {
	ID                    string
	CeremonyID            string
	ProfessionalUserID    string
	SessionDataCiphertext []byte
	PublicKey             json.RawMessage
	ConfirmationCode      string
	ExpiresAt             time.Time
}

func (r *Repository) CreateDeviceAuthorization(
	ctx context.Context,
	ceremonyID string,
	professionalUserID string,
	scanTokenHash string,
	pollTokenHash string,
	confirmationCode string,
	publicKey json.RawMessage,
	expiresAt time.Time,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO professional_device_authorizations (
			professional_user_id,
			webauthn_ceremony_id,
			scan_token_hash,
			poll_token_hash,
			confirmation_code,
			public_key,
			expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, professionalUserID, ceremonyID, scanTokenHash, pollTokenHash, confirmationCode, publicKey, expiresAt)
	if err != nil {
		return fmt.Errorf("create professional device authorization: %w", err)
	}
	return nil
}

func (r *Repository) GetDeviceAuthorizationByScanToken(
	ctx context.Context,
	scanTokenHash string,
	now time.Time,
) (storedDeviceAuthorization, error) {
	var stored storedDeviceAuthorization
	err := r.pool.QueryRow(ctx, `
		SELECT
			d.id::text,
			c.id::text,
			d.professional_user_id::text,
			c.session_data_ciphertext,
			d.public_key,
			d.confirmation_code,
			d.expires_at
		FROM professional_device_authorizations AS d
		JOIN professional_webauthn_ceremonies AS c
			ON c.id = d.webauthn_ceremony_id
		WHERE d.scan_token_hash = $1
		  AND d.approved_at IS NULL
		  AND d.consumed_at IS NULL
		  AND d.expires_at > $2
		  AND c.consumed_at IS NULL
		  AND c.expires_at > $2
	`, scanTokenHash, now).Scan(
		&stored.ID,
		&stored.CeremonyID,
		&stored.ProfessionalUserID,
		&stored.SessionDataCiphertext,
		&stored.PublicKey,
		&stored.ConfirmationCode,
		&stored.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return storedDeviceAuthorization{}, ErrInvalidToken
	}
	if err != nil {
		return storedDeviceAuthorization{}, fmt.Errorf("get professional device authorization: %w", err)
	}
	return stored, nil
}

func (r *Repository) CompleteDeviceAuthorizationAuthentication(
	ctx context.Context,
	deviceAuthorizationID string,
	ceremonyID string,
	professionalUserID string,
	credentialID []byte,
	credentialCiphertext []byte,
	client ClientInfo,
	now time.Time,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin device authorization transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	consumed, err := tx.Exec(ctx, `
		UPDATE professional_webauthn_ceremonies
		SET consumed_at = $3
		WHERE id = $1
		  AND professional_user_id = $2
		  AND purpose = 'authentication'
		  AND consumed_at IS NULL
		  AND expires_at > $3
	`, ceremonyID, professionalUserID, now)
	if err != nil {
		return fmt.Errorf("consume device WebAuthn ceremony: %w", err)
	}
	if consumed.RowsAffected() != 1 {
		return ErrInvalidToken
	}

	updated, err := tx.Exec(ctx, `
		UPDATE professional_webauthn_credentials
		SET credential_ciphertext = $3,
			last_used_ip = NULLIF($4, '')::inet,
			last_used_at = $5,
			updated_at = $5
		WHERE professional_user_id = $1
		  AND credential_id = $2
		  AND revoked_at IS NULL
	`, professionalUserID, credentialID, credentialCiphertext, client.IPAddress, now)
	if err != nil {
		return fmt.Errorf("update device-authorizing passkey: %w", err)
	}
	if updated.RowsAffected() != 1 {
		return ErrInvalidToken
	}

	approved, err := tx.Exec(ctx, `
		UPDATE professional_device_authorizations
		SET approved_at = $3
		WHERE id = $1
		  AND professional_user_id = $2
		  AND approved_at IS NULL
		  AND consumed_at IS NULL
		  AND expires_at > $3
	`, deviceAuthorizationID, professionalUserID, now)
	if err != nil {
		return fmt.Errorf("approve professional device authorization: %w", err)
	}
	if approved.RowsAffected() != 1 {
		return ErrInvalidToken
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit device authorization transaction: %w", err)
	}
	return nil
}

func (r *Repository) ConsumeDeviceAuthorizationAndCreateSession(
	ctx context.Context,
	pollTokenHash string,
	refreshTokenHash string,
	expiresAt time.Time,
	client ClientInfo,
	now time.Time,
) (string, string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", "", fmt.Errorf("begin device authorization consumption: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var authorizationID string
	var professionalUserID string
	var approved bool
	err = tx.QueryRow(ctx, `
		SELECT id::text, professional_user_id::text, (approved_at IS NOT NULL)
		FROM professional_device_authorizations
		WHERE poll_token_hash = $1
		  AND consumed_at IS NULL
		  AND expires_at > $2
		FOR UPDATE
	`, pollTokenHash, now).Scan(&authorizationID, &professionalUserID, &approved)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrInvalidToken
	}
	if err != nil {
		return "", "", fmt.Errorf("lock professional device authorization: %w", err)
	}
	if !approved {
		return "", "", ErrDeviceAuthorizationPending
	}

	var sessionID string
	err = tx.QueryRow(ctx, `
		INSERT INTO auth_sessions (
			audience,
			professional_user_id,
			refresh_token_hash,
			mfa_verified_at,
			created_ip,
			last_used_ip,
			user_agent,
			expires_at
		)
		VALUES (
			'professional',
			$1,
			$2,
			$3,
			NULLIF($4, '')::inet,
			NULLIF($4, '')::inet,
			NULLIF($5, ''),
			$6
		)
		RETURNING id::text
	`, professionalUserID, refreshTokenHash, now, client.IPAddress, client.UserAgent, expiresAt).Scan(&sessionID)
	if err != nil {
		return "", "", fmt.Errorf("create authorized desktop session: %w", err)
	}

	result, err := tx.Exec(ctx, `
		UPDATE professional_device_authorizations
		SET consumed_at = $2
		WHERE id = $1 AND consumed_at IS NULL
	`, authorizationID, now)
	if err != nil {
		return "", "", fmt.Errorf("consume professional device authorization: %w", err)
	}
	if result.RowsAffected() != 1 {
		return "", "", ErrInvalidToken
	}

	if err := tx.Commit(ctx); err != nil {
		return "", "", fmt.Errorf("commit device-authorized session: %w", err)
	}
	return sessionID, professionalUserID, nil
}
