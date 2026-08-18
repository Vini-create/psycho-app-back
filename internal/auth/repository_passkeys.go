package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const maxPasskeysPerProfessional = 10

type storedPasskeyCredential struct {
	ID                   string
	CredentialID         []byte
	CredentialCiphertext []byte
	Label                *string
	CreatedAt            time.Time
	LastUsedAt           *time.Time
}

type storedWebAuthnCeremony struct {
	ID                    string
	ProfessionalUserID    string
	Purpose               string
	SessionDataCiphertext []byte
	ExpiresAt             time.Time
}

func (r *Repository) HasActivePasskeys(ctx context.Context, professionalUserID string) (bool, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM professional_webauthn_credentials
			WHERE professional_user_id = $1 AND revoked_at IS NULL
		)
	`, professionalUserID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check professional passkeys: %w", err)
	}

	return exists, nil
}

func (r *Repository) ListStoredPasskeyCredentials(
	ctx context.Context,
	professionalUserID string,
) ([]storedPasskeyCredential, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id::text,
			credential_id,
			credential_ciphertext,
			label,
			created_at,
			last_used_at
		FROM professional_webauthn_credentials
		WHERE professional_user_id = $1 AND revoked_at IS NULL
		ORDER BY created_at ASC
	`, professionalUserID)
	if err != nil {
		return nil, fmt.Errorf("query professional passkeys: %w", err)
	}
	defer rows.Close()

	credentials := make([]storedPasskeyCredential, 0)
	for rows.Next() {
		var credential storedPasskeyCredential
		if err := rows.Scan(
			&credential.ID,
			&credential.CredentialID,
			&credential.CredentialCiphertext,
			&credential.Label,
			&credential.CreatedAt,
			&credential.LastUsedAt,
		); err != nil {
			return nil, fmt.Errorf("scan professional passkey: %w", err)
		}
		credentials = append(credentials, credential)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate professional passkeys: %w", err)
	}

	return credentials, nil
}

func (r *Repository) CreateWebAuthnCeremony(
	ctx context.Context,
	professionalUserID string,
	purpose string,
	tokenHash string,
	sessionDataCiphertext []byte,
	expiresAt time.Time,
	client ClientInfo,
	now time.Time,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin WebAuthn ceremony transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		UPDATE professional_webauthn_ceremonies
		SET consumed_at = $3
		WHERE professional_user_id = $1
		  AND purpose = $2
		  AND consumed_at IS NULL
	`, professionalUserID, purpose, now); err != nil {
		return fmt.Errorf("invalidate previous WebAuthn ceremonies: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO professional_webauthn_ceremonies (
			professional_user_id,
			purpose,
			token_hash,
			session_data_ciphertext,
			requested_ip,
			user_agent,
			expires_at
		)
		VALUES ($1, $2, $3, $4, NULLIF($5, '')::inet, NULLIF($6, ''), $7)
	`,
		professionalUserID,
		purpose,
		tokenHash,
		sessionDataCiphertext,
		client.IPAddress,
		client.UserAgent,
		expiresAt,
	); err != nil {
		return fmt.Errorf("insert WebAuthn ceremony: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit WebAuthn ceremony transaction: %w", err)
	}

	return nil
}

func (r *Repository) GetWebAuthnCeremony(
	ctx context.Context,
	professionalUserID string,
	purpose string,
	tokenHash string,
	now time.Time,
) (storedWebAuthnCeremony, error) {
	var ceremony storedWebAuthnCeremony
	err := r.pool.QueryRow(ctx, `
		SELECT
			id::text,
			professional_user_id::text,
			purpose,
			session_data_ciphertext,
			expires_at
		FROM professional_webauthn_ceremonies
		WHERE professional_user_id = $1
		  AND purpose = $2
		  AND token_hash = $3
		  AND consumed_at IS NULL
		  AND expires_at > $4
	`, professionalUserID, purpose, tokenHash, now).Scan(
		&ceremony.ID,
		&ceremony.ProfessionalUserID,
		&ceremony.Purpose,
		&ceremony.SessionDataCiphertext,
		&ceremony.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return storedWebAuthnCeremony{}, ErrInvalidToken
	}
	if err != nil {
		return storedWebAuthnCeremony{}, fmt.Errorf("get WebAuthn ceremony: %w", err)
	}

	return ceremony, nil
}

func (r *Repository) GetWebAuthnAuthenticationCeremony(
	ctx context.Context,
	tokenHash string,
	now time.Time,
) (storedWebAuthnCeremony, error) {
	var ceremony storedWebAuthnCeremony
	err := r.pool.QueryRow(ctx, `
		SELECT
			id::text,
			professional_user_id::text,
			purpose,
			session_data_ciphertext,
			expires_at
		FROM professional_webauthn_ceremonies
		WHERE purpose = 'authentication'
		  AND token_hash = $1
		  AND consumed_at IS NULL
		  AND expires_at > $2
	`, tokenHash, now).Scan(
		&ceremony.ID,
		&ceremony.ProfessionalUserID,
		&ceremony.Purpose,
		&ceremony.SessionDataCiphertext,
		&ceremony.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return storedWebAuthnCeremony{}, ErrInvalidToken
	}
	if err != nil {
		return storedWebAuthnCeremony{}, fmt.Errorf("get WebAuthn authentication ceremony: %w", err)
	}

	return ceremony, nil
}

func (r *Repository) CompletePasskeyRegistration(
	ctx context.Context,
	ceremonyID string,
	professionalUserID string,
	sessionID string,
	credentialID []byte,
	credentialCiphertext []byte,
	label string,
	recoveryCodeHashes []string,
	client ClientInfo,
	now time.Time,
) (storedPasskeyCredential, bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return storedPasskeyCredential{}, false, fmt.Errorf("begin passkey registration transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Serializing by account prevents two password-only sessions from both
	// becoming the first trusted passkey session concurrently.
	var lockedProfessionalID string
	if err := tx.QueryRow(ctx, `
		SELECT id::text
		FROM professional_users
		WHERE id = $1
		FOR UPDATE
	`, professionalUserID).Scan(&lockedProfessionalID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return storedPasskeyCredential{}, false, ErrNotFound
		}
		return storedPasskeyCredential{}, false, fmt.Errorf("lock professional for passkey registration: %w", err)
	}

	var sessionMFA bool
	if err := tx.QueryRow(ctx, `
		SELECT (mfa_verified_at IS NOT NULL)
		FROM auth_sessions
		WHERE id = $1
		  AND audience = 'professional'
		  AND professional_user_id = $2
		  AND revoked_at IS NULL
		  AND expires_at > $3
		FOR UPDATE
	`, sessionID, professionalUserID, now).Scan(&sessionMFA); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return storedPasskeyCredential{}, false, ErrInvalidToken
		}
		return storedPasskeyCredential{}, false, fmt.Errorf("lock passkey registration session: %w", err)
	}

	var activeCredentialCount int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM professional_webauthn_credentials
		WHERE professional_user_id = $1 AND revoked_at IS NULL
	`, professionalUserID).Scan(&activeCredentialCount); err != nil {
		return storedPasskeyCredential{}, false, fmt.Errorf("count professional passkeys: %w", err)
	}
	if activeCredentialCount > 0 && !sessionMFA {
		return storedPasskeyCredential{}, false, ErrForbidden
	}
	if activeCredentialCount >= maxPasskeysPerProfessional {
		return storedPasskeyCredential{}, false, ErrPasskeyLimit
	}

	consumed, err := tx.Exec(ctx, `
		UPDATE professional_webauthn_ceremonies
		SET consumed_at = $3
		WHERE id = $1
		  AND professional_user_id = $2
		  AND purpose = 'registration'
		  AND consumed_at IS NULL
		  AND expires_at > $3
	`, ceremonyID, professionalUserID, now)
	if err != nil {
		return storedPasskeyCredential{}, false, fmt.Errorf("consume passkey registration ceremony: %w", err)
	}
	if consumed.RowsAffected() != 1 {
		return storedPasskeyCredential{}, false, ErrInvalidToken
	}

	var created storedPasskeyCredential
	if err := tx.QueryRow(ctx, `
		INSERT INTO professional_webauthn_credentials (
			professional_user_id,
			credential_id,
			credential_ciphertext,
			label,
			created_ip
		)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, '')::inet)
		RETURNING id::text, credential_id, credential_ciphertext, label, created_at, last_used_at
	`, professionalUserID, credentialID, credentialCiphertext, label, client.IPAddress).Scan(
		&created.ID,
		&created.CredentialID,
		&created.CredentialCiphertext,
		&created.Label,
		&created.CreatedAt,
		&created.LastUsedAt,
	); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return storedPasskeyCredential{}, false, ErrPasskeyExists
		}
		return storedPasskeyCredential{}, false, fmt.Errorf("insert professional passkey: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE auth_sessions
		SET mfa_verified_at = $3, last_used_at = $3
		WHERE id = $1 AND professional_user_id = $2
	`, sessionID, professionalUserID, now); err != nil {
		return storedPasskeyCredential{}, false, fmt.Errorf("elevate passkey registration session: %w", err)
	}

	recoveryCodesCreated := activeCredentialCount == 0
	if recoveryCodesCreated {
		// Other sessions only proved the password before the first passkey
		// existed. Revoke them when the account becomes passkey-protected.
		if _, err := tx.Exec(ctx, `
			UPDATE auth_sessions
			SET revoked_at = $3, revoke_reason = 'passkey_enrolled'
			WHERE professional_user_id = $1
			  AND id <> $2
			  AND mfa_verified_at IS NULL
			  AND revoked_at IS NULL
		`, professionalUserID, sessionID, now); err != nil {
			return storedPasskeyCredential{}, false, fmt.Errorf("revoke pre-passkey sessions: %w", err)
		}

		if _, err := tx.Exec(ctx, `
			DELETE FROM professional_recovery_codes
			WHERE professional_user_id = $1
		`, professionalUserID); err != nil {
			return storedPasskeyCredential{}, false, fmt.Errorf("clear previous professional recovery codes: %w", err)
		}

		for _, codeHash := range recoveryCodeHashes {
			if _, err := tx.Exec(ctx, `
				INSERT INTO professional_recovery_codes (
					professional_user_id,
					code_hash
				)
				VALUES ($1, $2)
			`, professionalUserID, codeHash); err != nil {
				return storedPasskeyCredential{}, false, fmt.Errorf("insert professional recovery code: %w", err)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return storedPasskeyCredential{}, false, fmt.Errorf("commit passkey registration transaction: %w", err)
	}

	return created, recoveryCodesCreated, nil
}

func (r *Repository) CompletePasskeyAuthentication(
	ctx context.Context,
	ceremonyID string,
	professionalUserID string,
	credentialID []byte,
	credentialCiphertext []byte,
	client ClientInfo,
	now time.Time,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin passkey authentication transaction: %w", err)
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
		return fmt.Errorf("consume passkey authentication ceremony: %w", err)
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
		return fmt.Errorf("update authenticated passkey: %w", err)
	}
	if updated.RowsAffected() != 1 {
		return ErrInvalidToken
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit passkey authentication transaction: %w", err)
	}

	return nil
}

func (r *Repository) UseProfessionalRecoveryCode(
	ctx context.Context,
	ceremonyID string,
	professionalUserID string,
	codeHash string,
	now time.Time,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin professional recovery transaction: %w", err)
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
		return fmt.Errorf("consume recovery ceremony: %w", err)
	}
	if consumed.RowsAffected() != 1 {
		return ErrInvalidToken
	}

	used, err := tx.Exec(ctx, `
		UPDATE professional_recovery_codes
		SET used_at = $3
		WHERE professional_user_id = $1
		  AND code_hash = $2
		  AND used_at IS NULL
	`, professionalUserID, codeHash, now)
	if err != nil {
		return fmt.Errorf("consume professional recovery code: %w", err)
	}
	if used.RowsAffected() != 1 {
		return ErrInvalidRecoveryCode
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit professional recovery transaction: %w", err)
	}

	return nil
}

func (r *Repository) ReplaceProfessionalRecoveryCodes(
	ctx context.Context,
	professionalUserID string,
	recoveryCodeHashes []string,
	now time.Time,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin professional recovery codes transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		DELETE FROM professional_recovery_codes
		WHERE professional_user_id = $1
	`, professionalUserID); err != nil {
		return fmt.Errorf("delete professional recovery codes: %w", err)
	}

	for _, codeHash := range recoveryCodeHashes {
		if _, err := tx.Exec(ctx, `
			INSERT INTO professional_recovery_codes (
				professional_user_id,
				code_hash,
				created_at
			)
			VALUES ($1, $2, $3)
		`, professionalUserID, codeHash, now); err != nil {
			return fmt.Errorf("insert regenerated professional recovery code: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit professional recovery codes transaction: %w", err)
	}

	return nil
}

func (r *Repository) RevokePasskey(
	ctx context.Context,
	professionalUserID string,
	passkeyID string,
	now time.Time,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin passkey revocation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		SELECT id
		FROM professional_webauthn_credentials
		WHERE professional_user_id = $1 AND revoked_at IS NULL
		FOR UPDATE
	`, professionalUserID)
	if err != nil {
		return fmt.Errorf("count passkeys before revocation: %w", err)
	}
	activeCount := 0
	for rows.Next() {
		activeCount++
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate passkeys before revocation: %w", err)
	}
	rows.Close()
	if activeCount <= 1 {
		return ErrLastPasskey
	}

	result, err := tx.Exec(ctx, `
		UPDATE professional_webauthn_credentials
		SET revoked_at = $3, updated_at = $3
		WHERE id = $1
		  AND professional_user_id = $2
		  AND revoked_at IS NULL
	`, passkeyID, professionalUserID, now)
	if err != nil {
		return fmt.Errorf("revoke professional passkey: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit passkey revocation transaction: %w", err)
	}

	return nil
}
