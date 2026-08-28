package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (r *Repository) CreateAccount(
	ctx context.Context,
	audience Audience,
	email string,
	passwordHash string,
	displayName string,
	verificationTokenHash string,
	verificationExpiresAt time.Time,
	emailMessage EmailOutboxMessage,
	client ClientInfo,
) (string, error) {
	table, identityColumn, err := accountTable(audience)
	if err != nil {
		return "", err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin account transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	query := fmt.Sprintf(`
		INSERT INTO %s (email, password_hash, display_name)
		VALUES ($1, $2, $3)
		RETURNING id::text
	`, table)

	var accountID string
	if err := tx.QueryRow(ctx, query, email, passwordHash, displayName).Scan(&accountID); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return "", ErrConflict
		}
		return "", fmt.Errorf("insert account: %w", err)
	}
	if audience == AudienceProfessional {
		if err := ensureProfessionalWorkspace(ctx, tx, accountID, displayName); err != nil {
			return "", err
		}
	}

	insertTokenQuery := fmt.Sprintf(`
		INSERT INTO auth_one_time_tokens (
			audience,
			%s,
			purpose,
			token_hash,
			requested_ip,
			user_agent,
			expires_at
		)
		VALUES ($1, $2, 'email_verification', $3, NULLIF($4, '')::inet, NULLIF($5, ''), $6)
	`, identityColumn)

	if _, err := tx.Exec(
		ctx,
		insertTokenQuery,
		audience,
		accountID,
		verificationTokenHash,
		client.IPAddress,
		client.UserAgent,
		verificationExpiresAt,
	); err != nil {
		return "", fmt.Errorf("insert verification token: %w", err)
	}
	if err := insertEmailOutbox(ctx, tx, audience, emailMessage); err != nil {
		return "", err
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit account transaction: %w", err)
	}

	return accountID, nil
}

func (r *Repository) FindAccountByEmail(
	ctx context.Context,
	audience Audience,
	email string,
) (Account, error) {
	table, _, err := accountTable(audience)
	if err != nil {
		return Account{}, err
	}

	query := fmt.Sprintf(`
		SELECT id::text, email, password_hash, display_name, status, email_verified_at
		FROM %s
		WHERE email = $1 AND deleted_at IS NULL
	`, table)

	var account Account
	err = r.pool.QueryRow(ctx, query, email).Scan(
		&account.ID,
		&account.Email,
		&account.PasswordHash,
		&account.DisplayName,
		&account.Status,
		&account.EmailVerifiedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("find account by email: %w", err)
	}

	return account, nil
}

func (r *Repository) FindAccountByID(
	ctx context.Context,
	audience Audience,
	accountID string,
) (Account, error) {
	table, identityColumn, err := accountTable(audience)
	if err != nil {
		return Account{}, err
	}

	planExpression := "''"
	if audience == AudienceApp {
		planExpression = "account.plan"
	} else if audience == AudienceProfessional {
		planExpression = `COALESCE((
			SELECT subscription.plan
			FROM organization_memberships AS membership
			JOIN subscriptions AS subscription
			  ON subscription.organization_id = membership.organization_id
			WHERE membership.professional_user_id = account.id
			  AND membership.status = 'active'
			ORDER BY membership.created_at
			LIMIT 1
		), '')`
	}
	query := fmt.Sprintf(`
		SELECT account.id::text, account.email, account.password_hash,
		       account.display_name, account.status, account.email_verified_at,
		       account.created_at, account.updated_at, %s,
		       EXISTS (
		           SELECT 1
		           FROM auth_external_identities AS identity
		           WHERE identity.audience = $2
		             AND identity.provider = 'google'
		             AND identity.%s = account.id
		       )
		FROM %s AS account
		WHERE account.id = $1 AND account.deleted_at IS NULL
	`, planExpression, identityColumn, table)

	var account Account
	err = r.pool.QueryRow(ctx, query, accountID, audience).Scan(
		&account.ID,
		&account.Email,
		&account.PasswordHash,
		&account.DisplayName,
		&account.Status,
		&account.EmailVerifiedAt,
		&account.CreatedAt,
		&account.UpdatedAt,
		&account.Plan,
		&account.GoogleConnected,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("find account by ID: %w", err)
	}

	return account, nil
}

func ensureProfessionalWorkspace(
	ctx context.Context,
	tx pgx.Tx,
	professionalUserID string,
	displayName string,
) error {
	var activeMembershipExists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM organization_memberships
			WHERE professional_user_id = $1 AND status = 'active'
		)
	`, professionalUserID).Scan(&activeMembershipExists); err != nil {
		return fmt.Errorf("check professional workspace: %w", err)
	}
	if activeMembershipExists {
		return nil
	}

	var organizationID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO organizations (name, kind)
		VALUES ($1, 'solo')
		RETURNING id::text
	`, "Consultório de "+displayName).Scan(&organizationID); err != nil {
		return fmt.Errorf("create professional organization: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO organization_memberships (
			organization_id, professional_user_id, role, status, joined_at
		)
		VALUES ($1, $2, 'owner', 'active', now())
	`, organizationID, professionalUserID); err != nil {
		return fmt.Errorf("create professional membership: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO subscriptions (organization_id, provider, plan, status)
		VALUES ($1, 'internal', 'pro', 'active')
	`, organizationID); err != nil {
		return fmt.Errorf("grant professional Pro plan: %w", err)
	}
	return nil
}

func (r *Repository) UpdateDisplayName(
	ctx context.Context,
	audience Audience,
	accountID string,
	displayName string,
	now time.Time,
) error {
	table, _, err := accountTable(audience)
	if err != nil {
		return err
	}
	query := fmt.Sprintf(`
		UPDATE %s
		SET display_name = $2, updated_at = $3
		WHERE id = $1 AND deleted_at IS NULL
	`, table)
	result, err := r.pool.Exec(ctx, query, accountID, displayName, now)
	if err != nil {
		return fmt.Errorf("update account display name: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) ChangePassword(
	ctx context.Context,
	principal Principal,
	expectedPasswordHash string,
	newPasswordHash string,
	now time.Time,
) error {
	table, identityColumn, err := accountTable(principal.Audience)
	if err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin password change transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	updateQuery := fmt.Sprintf(`
		UPDATE %s
		SET password_hash = $3, updated_at = $4
		WHERE id = $1 AND password_hash = $2 AND deleted_at IS NULL
	`, table)
	result, err := tx.Exec(
		ctx, updateQuery, principal.AccountID, expectedPasswordHash, newPasswordHash, now,
	)
	if err != nil {
		return fmt.Errorf("change account password: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrInvalidCredentials
	}

	revokeQuery := fmt.Sprintf(`
		UPDATE auth_sessions
		SET revoked_at = COALESCE(revoked_at, $4),
		    revoke_reason = COALESCE(revoke_reason, 'password_changed')
		WHERE audience = $1
		  AND %s = $2
		  AND token_family_id <> (
		      SELECT token_family_id
		      FROM auth_sessions
		      WHERE id = $3 AND audience = $1 AND %s = $2
		  )
		  AND revoked_at IS NULL
	`, identityColumn, identityColumn)
	if _, err := tx.Exec(
		ctx, revokeQuery, principal.Audience, principal.AccountID, principal.SessionID, now,
	); err != nil {
		return fmt.Errorf("revoke other sessions after password change: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit password change transaction: %w", err)
	}
	return nil
}

func (r *Repository) ReplaceOneTimeTokenByEmail(
	ctx context.Context,
	audience Audience,
	email string,
	purpose string,
	tokenHash string,
	expiresAt time.Time,
	emailMessage EmailOutboxMessage,
	client ClientInfo,
) (bool, error) {
	table, identityColumn, err := accountTable(audience)
	if err != nil {
		return false, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin one-time token transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	findQuery := fmt.Sprintf(`
		SELECT id::text
		FROM %s
		WHERE email = $1
		  AND deleted_at IS NULL
		  AND (
			($2 = 'email_verification' AND status = 'pending_verification')
			OR ($2 = 'password_reset' AND status = 'active')
		  )
	`, table)

	var accountID string
	if err := tx.QueryRow(ctx, findQuery, email, purpose).Scan(&accountID); errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("find account for one-time token: %w", err)
	}

	invalidateQuery := fmt.Sprintf(`
		UPDATE auth_one_time_tokens
		SET consumed_at = now()
		WHERE audience = $1
		  AND %s = $2
		  AND purpose = $3
		  AND consumed_at IS NULL
	`, identityColumn)

	if _, err := tx.Exec(ctx, invalidateQuery, audience, accountID, purpose); err != nil {
		return false, fmt.Errorf("invalidate previous one-time tokens: %w", err)
	}

	insertQuery := fmt.Sprintf(`
		INSERT INTO auth_one_time_tokens (
			audience,
			%s,
			purpose,
			token_hash,
			requested_ip,
			user_agent,
			expires_at
		)
		VALUES ($1, $2, $3, $4, NULLIF($5, '')::inet, NULLIF($6, ''), $7)
	`, identityColumn)

	if _, err := tx.Exec(
		ctx,
		insertQuery,
		audience,
		accountID,
		purpose,
		tokenHash,
		client.IPAddress,
		client.UserAgent,
		expiresAt,
	); err != nil {
		return false, fmt.Errorf("insert one-time token: %w", err)
	}
	if err := insertEmailOutbox(ctx, tx, audience, emailMessage); err != nil {
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit one-time token transaction: %w", err)
	}

	return true, nil
}

type emailOutboxExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func insertEmailOutbox(
	ctx context.Context,
	executor emailOutboxExecutor,
	audience Audience,
	message EmailOutboxMessage,
) error {
	if message.Kind == "" || message.RecipientEmail == "" || len(message.TokenCiphertext) == 0 {
		return fmt.Errorf("email outbox message is required")
	}
	if _, err := executor.Exec(ctx, `
		INSERT INTO auth_email_outbox (
			audience, kind, recipient_email, token_ciphertext
		)
		VALUES ($1, $2, $3, $4)
	`, audience, message.Kind, message.RecipientEmail, message.TokenCiphertext); err != nil {
		return fmt.Errorf("insert email outbox message: %w", err)
	}
	return nil
}

func (r *Repository) CreateOneTimeTokenForAccount(
	ctx context.Context,
	audience Audience,
	accountID string,
	purpose string,
	tokenHash string,
	expiresAt time.Time,
	client ClientInfo,
) error {
	_, identityColumn, err := accountTable(audience)
	if err != nil {
		return err
	}

	query := fmt.Sprintf(`
		INSERT INTO auth_one_time_tokens (
			audience,
			%s,
			purpose,
			token_hash,
			requested_ip,
			user_agent,
			expires_at
		)
		VALUES ($1, $2, $3, $4, NULLIF($5, '')::inet, NULLIF($6, ''), $7)
	`, identityColumn)

	if _, err := r.pool.Exec(
		ctx,
		query,
		audience,
		accountID,
		purpose,
		tokenHash,
		client.IPAddress,
		client.UserAgent,
		expiresAt,
	); err != nil {
		return fmt.Errorf("create one-time token: %w", err)
	}

	return nil
}

func (r *Repository) VerifyEmail(
	ctx context.Context,
	audience Audience,
	tokenHash string,
	now time.Time,
) error {
	table, identityColumn, err := accountTable(audience)
	if err != nil {
		return err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin email verification transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	selectQuery := fmt.Sprintf(`
		SELECT id::text, %s::text, expires_at, consumed_at
		FROM auth_one_time_tokens
		WHERE audience = $1 AND purpose = 'email_verification' AND token_hash = $2
		FOR UPDATE
	`, identityColumn)

	var tokenID string
	var accountID string
	var expiresAt time.Time
	var consumedAt *time.Time
	if err := tx.QueryRow(ctx, selectQuery, audience, tokenHash).Scan(
		&tokenID,
		&accountID,
		&expiresAt,
		&consumedAt,
	); errors.Is(err, pgx.ErrNoRows) {
		return ErrInvalidToken
	} else if err != nil {
		return fmt.Errorf("select email verification token: %w", err)
	}

	if consumedAt != nil || !now.Before(expiresAt) {
		return ErrInvalidToken
	}

	updateAccountQuery := fmt.Sprintf(`
		UPDATE %s
		SET status = 'active', email_verified_at = $2, updated_at = $2
		WHERE id = $1 AND status = 'pending_verification'
	`, table)

	result, err := tx.Exec(ctx, updateAccountQuery, accountID, now)
	if err != nil {
		return fmt.Errorf("verify account email: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrInvalidToken
	}

	if _, err := tx.Exec(
		ctx,
		`UPDATE auth_one_time_tokens SET consumed_at = $2 WHERE id = $1`,
		tokenID,
		now,
	); err != nil {
		return fmt.Errorf("consume email verification token: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit email verification transaction: %w", err)
	}

	return nil
}

func (r *Repository) ResetPassword(
	ctx context.Context,
	audience Audience,
	tokenHash string,
	passwordHash string,
	now time.Time,
) error {
	table, identityColumn, err := accountTable(audience)
	if err != nil {
		return err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin password reset transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	selectQuery := fmt.Sprintf(`
		SELECT id::text, %s::text, expires_at, consumed_at
		FROM auth_one_time_tokens
		WHERE audience = $1 AND purpose = 'password_reset' AND token_hash = $2
		FOR UPDATE
	`, identityColumn)

	var tokenID string
	var accountID string
	var expiresAt time.Time
	var consumedAt *time.Time
	if err := tx.QueryRow(ctx, selectQuery, audience, tokenHash).Scan(
		&tokenID,
		&accountID,
		&expiresAt,
		&consumedAt,
	); errors.Is(err, pgx.ErrNoRows) {
		return ErrInvalidToken
	} else if err != nil {
		return fmt.Errorf("select password reset token: %w", err)
	}

	if consumedAt != nil || !now.Before(expiresAt) {
		return ErrInvalidToken
	}

	updateAccountQuery := fmt.Sprintf(`
		UPDATE %s
		SET password_hash = $2, updated_at = $3
		WHERE id = $1 AND deleted_at IS NULL
	`, table)

	if _, err := tx.Exec(ctx, updateAccountQuery, accountID, passwordHash, now); err != nil {
		return fmt.Errorf("update account password: %w", err)
	}

	if _, err := tx.Exec(
		ctx,
		`UPDATE auth_one_time_tokens SET consumed_at = $2 WHERE id = $1`,
		tokenID,
		now,
	); err != nil {
		return fmt.Errorf("consume password reset token: %w", err)
	}

	revokeQuery := fmt.Sprintf(`
		UPDATE auth_sessions
		SET revoked_at = $3, revoke_reason = 'password_reset'
		WHERE audience = $1 AND %s = $2 AND revoked_at IS NULL
	`, identityColumn)

	if _, err := tx.Exec(ctx, revokeQuery, audience, accountID, now); err != nil {
		return fmt.Errorf("revoke sessions after password reset: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit password reset transaction: %w", err)
	}

	return nil
}
