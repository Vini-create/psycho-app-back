package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (r *Repository) CreateGoogleChallenge(
	ctx context.Context,
	audience Audience,
	nonceHash string,
	expiresAt time.Time,
	client ClientInfo,
) (string, error) {
	var id string
	err := r.pool.QueryRow(ctx, `
		INSERT INTO auth_google_challenges (
			audience, nonce_hash, requested_ip, user_agent, expires_at
		)
		VALUES ($1, $2, NULLIF($3, '')::inet, NULLIF($4, ''), $5)
		RETURNING id::text
	`, audience, nonceHash, client.IPAddress, client.UserAgent, expiresAt).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create Google challenge: %w", err)
	}
	return id, nil
}

func (r *Repository) ConsumeGoogleChallenge(
	ctx context.Context,
	audience Audience,
	challengeID string,
	nonceHash string,
	now time.Time,
) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE auth_google_challenges
		SET consumed_at = $4
		WHERE id = $1
		  AND audience = $2
		  AND nonce_hash = $3
		  AND consumed_at IS NULL
		  AND expires_at > $4
	`, challengeID, audience, nonceHash, now)
	if err != nil {
		return fmt.Errorf("consume Google challenge: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrInvalidGoogleCredential
	}
	return nil
}

func (r *Repository) FindOrCreateGoogleAccount(
	ctx context.Context,
	audience Audience,
	identity GoogleIdentity,
	displayName string,
	passwordHash string,
	now time.Time,
) (Account, bool, error) {
	table, identityColumn, err := accountTable(audience)
	if err != nil {
		return Account{}, false, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Account{}, false, fmt.Errorf("begin Google account transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	lookupIdentity := fmt.Sprintf(`
		SELECT account.id::text, account.email, account.password_hash,
		       account.display_name, account.status, account.email_verified_at
		FROM auth_external_identities AS identity
		JOIN %s AS account ON account.id = identity.%s
		WHERE identity.audience = $1
		  AND identity.provider = 'google'
		  AND identity.subject = $2
		  AND account.deleted_at IS NULL
		FOR UPDATE OF account
	`, table, identityColumn)
	var account Account
	err = tx.QueryRow(ctx, lookupIdentity, audience, identity.Subject).Scan(
		&account.ID, &account.Email, &account.PasswordHash, &account.DisplayName,
		&account.Status, &account.EmailVerifiedAt,
	)
	if err == nil {
		if _, err := tx.Exec(ctx, `
			UPDATE auth_external_identities
			SET provider_email = $3, updated_at = $4
			WHERE audience = $1 AND provider = 'google' AND subject = $2
		`, audience, identity.Subject, identity.Email, now); err != nil {
			return Account{}, false, fmt.Errorf("update Google identity: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return Account{}, false, fmt.Errorf("commit Google identity lookup: %w", err)
		}
		return account, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Account{}, false, fmt.Errorf("find Google identity: %w", err)
	}

	findByEmail := fmt.Sprintf(`
		SELECT id::text, email, password_hash, display_name, status, email_verified_at
		FROM %s
		WHERE email = $1 AND deleted_at IS NULL
		FOR UPDATE
	`, table)
	err = tx.QueryRow(ctx, findByEmail, identity.Email).Scan(
		&account.ID, &account.Email, &account.PasswordHash, &account.DisplayName,
		&account.Status, &account.EmailVerifiedAt,
	)
	created := false
	if err == nil {
		if !identity.AuthoritativeEmail {
			return Account{}, false, ErrGoogleLinkRequired
		}
		if account.Status == "pending_verification" {
			activate := fmt.Sprintf(`
				UPDATE %s SET status = 'active', email_verified_at = $2, updated_at = $2
				WHERE id = $1
			`, table)
			if _, err := tx.Exec(ctx, activate, account.ID, now); err != nil {
				return Account{}, false, fmt.Errorf("activate Google account: %w", err)
			}
			account.Status = "active"
			account.EmailVerifiedAt = &now
		}
	} else if errors.Is(err, pgx.ErrNoRows) {
		insertAccount := fmt.Sprintf(`
			INSERT INTO %s (
				email, password_hash, display_name, status, email_verified_at
			)
			VALUES ($1, $2, $3, 'active', $4)
			RETURNING id::text, email, password_hash, display_name, status, email_verified_at
		`, table)
		err = tx.QueryRow(ctx, insertAccount, identity.Email, passwordHash, displayName, now).Scan(
			&account.ID, &account.Email, &account.PasswordHash, &account.DisplayName,
			&account.Status, &account.EmailVerifiedAt,
		)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return Account{}, false, ErrConflict
			}
			return Account{}, false, fmt.Errorf("insert Google account: %w", err)
		}
		created = true
	} else {
		return Account{}, false, fmt.Errorf("find account for Google identity: %w", err)
	}

	insertIdentity := fmt.Sprintf(`
		INSERT INTO auth_external_identities (
			audience, %s, provider, subject, provider_email
		)
		VALUES ($1, $2, 'google', $3, $4)
	`, identityColumn)
	if _, err := tx.Exec(ctx, insertIdentity, audience, account.ID, identity.Subject, identity.Email); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Account{}, false, ErrConflict
		}
		return Account{}, false, fmt.Errorf("insert Google identity: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Account{}, false, fmt.Errorf("commit Google account transaction: %w", err)
	}
	return account, created, nil
}
