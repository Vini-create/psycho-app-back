package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type sessionIdentity struct {
	ID        string
	FamilyID  string
	Audience  Audience
	AccountID string
	MFA       bool
	ExpiresAt time.Time
	RotatedAt *time.Time
	RevokedAt *time.Time
}

func (r *Repository) CreateSession(
	ctx context.Context,
	audience Audience,
	accountID string,
	refreshTokenHash string,
	mfa bool,
	expiresAt time.Time,
	client ClientInfo,
) (string, error) {
	_, identityColumn, err := accountTable(audience)
	if err != nil {
		return "", err
	}

	query := fmt.Sprintf(`
		INSERT INTO auth_sessions (
			audience,
			%s,
			refresh_token_hash,
			mfa_verified_at,
			created_ip,
			last_used_ip,
			user_agent,
			expires_at
		)
		VALUES (
			$1,
			$2,
			$3,
			CASE WHEN $4 THEN now() ELSE NULL END,
			NULLIF($5, '')::inet,
			NULLIF($5, '')::inet,
			NULLIF($6, ''),
			$7
		)
		RETURNING id::text
	`, identityColumn)

	var sessionID string
	if err := r.pool.QueryRow(
		ctx,
		query,
		audience,
		accountID,
		refreshTokenHash,
		mfa,
		client.IPAddress,
		client.UserAgent,
		expiresAt,
	).Scan(&sessionID); err != nil {
		return "", fmt.Errorf("create auth session: %w", err)
	}

	return sessionID, nil
}

func (r *Repository) RotateSession(
	ctx context.Context,
	expectedAudience Audience,
	currentTokenHash string,
	newTokenHash string,
	newExpiresAt time.Time,
	client ClientInfo,
	now time.Time,
) (sessionIdentity, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return sessionIdentity{}, fmt.Errorf("begin refresh rotation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var current sessionIdentity
	var appUserID *string
	var professionalUserID *string
	err = tx.QueryRow(ctx, `
		SELECT
			id::text,
			token_family_id::text,
			audience,
			app_user_id::text,
			professional_user_id::text,
			(mfa_verified_at IS NOT NULL),
			expires_at,
			rotated_at,
			revoked_at
		FROM auth_sessions
		WHERE refresh_token_hash = $1 AND audience = $2
		FOR UPDATE
	`, currentTokenHash, expectedAudience).Scan(
		&current.ID,
		&current.FamilyID,
		&current.Audience,
		&appUserID,
		&professionalUserID,
		&current.MFA,
		&current.ExpiresAt,
		&current.RotatedAt,
		&current.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return sessionIdentity{}, ErrInvalidRefreshToken
	}
	if err != nil {
		return sessionIdentity{}, fmt.Errorf("select refresh session: %w", err)
	}

	if current.Audience == AudienceApp && appUserID != nil {
		current.AccountID = *appUserID
	} else if current.Audience == AudienceProfessional && professionalUserID != nil {
		current.AccountID = *professionalUserID
	} else {
		return sessionIdentity{}, ErrInvalidRefreshToken
	}

	if current.RotatedAt != nil || current.RevokedAt != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE auth_sessions
			SET revoked_at = COALESCE(revoked_at, $2),
				revoke_reason = COALESCE(revoke_reason, 'refresh_reuse')
			WHERE token_family_id = $1
		`, current.FamilyID, now); err != nil {
			return sessionIdentity{}, fmt.Errorf("revoke reused refresh family: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return sessionIdentity{}, fmt.Errorf("commit refresh reuse revocation: %w", err)
		}
		return sessionIdentity{}, ErrInvalidRefreshToken
	}

	if !now.Before(current.ExpiresAt) {
		if _, err := tx.Exec(ctx, `
			UPDATE auth_sessions
			SET revoked_at = $2, revoke_reason = 'expired'
			WHERE id = $1
		`, current.ID, now); err != nil {
			return sessionIdentity{}, fmt.Errorf("revoke expired refresh session: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return sessionIdentity{}, fmt.Errorf("commit expired refresh revocation: %w", err)
		}
		return sessionIdentity{}, ErrInvalidRefreshToken
	}

	_, identityColumn, err := accountTable(current.Audience)
	if err != nil {
		return sessionIdentity{}, err
	}

	insertQuery := fmt.Sprintf(`
		INSERT INTO auth_sessions (
			audience,
			%s,
			token_family_id,
			refresh_token_hash,
			mfa_verified_at,
			created_ip,
			last_used_ip,
			user_agent,
			expires_at
		)
		VALUES (
			$1,
			$2,
			$3,
			$4,
			CASE WHEN $5 THEN $6::timestamptz ELSE NULL END,
			NULLIF($7, '')::inet,
			NULLIF($7, '')::inet,
			NULLIF($8, ''),
			$9
		)
		RETURNING id::text
	`, identityColumn)

	var newSessionID string
	if err := tx.QueryRow(
		ctx,
		insertQuery,
		current.Audience,
		current.AccountID,
		current.FamilyID,
		newTokenHash,
		current.MFA,
		now,
		client.IPAddress,
		client.UserAgent,
		newExpiresAt,
	).Scan(&newSessionID); err != nil {
		return sessionIdentity{}, fmt.Errorf("insert rotated auth session: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE auth_sessions
		SET rotated_at = $2,
			replaced_by_session_id = $3,
			last_used_at = $2,
			last_used_ip = NULLIF($4, '')::inet
		WHERE id = $1
	`, current.ID, now, newSessionID, client.IPAddress); err != nil {
		return sessionIdentity{}, fmt.Errorf("mark auth session as rotated: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return sessionIdentity{}, fmt.Errorf("commit refresh rotation: %w", err)
	}

	current.ID = newSessionID
	current.ExpiresAt = newExpiresAt
	current.RotatedAt = nil
	return current, nil
}

func (r *Repository) SessionIsActive(
	ctx context.Context,
	principal Principal,
	now time.Time,
) (bool, error) {
	_, identityColumn, err := accountTable(principal.Audience)
	if err != nil {
		return false, err
	}

	query := fmt.Sprintf(`
		SELECT EXISTS (
			SELECT 1
			FROM auth_sessions
			WHERE id = $1
			  AND audience = $2
			  AND %s = $3
			  AND revoked_at IS NULL
			  AND expires_at > $4
		)
	`, identityColumn)

	var active bool
	if err := r.pool.QueryRow(
		ctx,
		query,
		principal.SessionID,
		principal.Audience,
		principal.AccountID,
		now,
	).Scan(&active); err != nil {
		return false, fmt.Errorf("check auth session: %w", err)
	}

	return active, nil
}

func (r *Repository) RevokeSessionFamily(
	ctx context.Context,
	principal Principal,
	sessionID string,
	reason string,
	now time.Time,
) (bool, error) {
	_, identityColumn, err := accountTable(principal.Audience)
	if err != nil {
		return false, err
	}

	query := fmt.Sprintf(`
		UPDATE auth_sessions
		SET revoked_at = COALESCE(revoked_at, $5),
			revoke_reason = COALESCE(revoke_reason, $4)
		WHERE token_family_id = (
			SELECT token_family_id
			FROM auth_sessions
			WHERE id = $1 AND audience = $2 AND %s = $3
		)
	`, identityColumn)

	result, err := r.pool.Exec(
		ctx,
		query,
		sessionID,
		principal.Audience,
		principal.AccountID,
		reason,
		now,
	)
	if err != nil {
		return false, fmt.Errorf("revoke auth session family: %w", err)
	}

	return result.RowsAffected() > 0, nil
}

func (r *Repository) RevokeAllSessions(
	ctx context.Context,
	principal Principal,
	reason string,
	now time.Time,
) error {
	_, identityColumn, err := accountTable(principal.Audience)
	if err != nil {
		return err
	}

	query := fmt.Sprintf(`
		UPDATE auth_sessions
		SET revoked_at = COALESCE(revoked_at, $4),
			revoke_reason = COALESCE(revoke_reason, $3)
		WHERE audience = $1 AND %s = $2 AND revoked_at IS NULL
	`, identityColumn)

	if _, err := r.pool.Exec(
		ctx,
		query,
		principal.Audience,
		principal.AccountID,
		reason,
		now,
	); err != nil {
		return fmt.Errorf("revoke all auth sessions: %w", err)
	}

	return nil
}

func (r *Repository) ListSessions(
	ctx context.Context,
	principal Principal,
	now time.Time,
) ([]Session, error) {
	_, identityColumn, err := accountTable(principal.Audience)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT
			id::text,
			created_ip::text,
			last_used_ip::text,
			user_agent,
			(mfa_verified_at IS NOT NULL),
			expires_at,
			last_used_at,
			created_at,
			revoked_at
		FROM auth_sessions
		WHERE audience = $1
		  AND %s = $2
		  AND rotated_at IS NULL
		  AND expires_at > $3
		ORDER BY created_at DESC
	`, identityColumn)

	rows, err := r.pool.Query(ctx, query, principal.Audience, principal.AccountID, now)
	if err != nil {
		return nil, fmt.Errorf("query auth sessions: %w", err)
	}
	defer rows.Close()

	sessions := make([]Session, 0)
	for rows.Next() {
		var session Session
		if err := rows.Scan(
			&session.ID,
			&session.CreatedIP,
			&session.LastUsedIP,
			&session.UserAgent,
			&session.MFA,
			&session.ExpiresAt,
			&session.LastUsedAt,
			&session.CreatedAt,
			&session.RevokedAt,
		); err != nil {
			return nil, fmt.Errorf("scan auth session: %w", err)
		}
		session.CurrentSession = session.ID == principal.SessionID
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate auth sessions: %w", err)
	}

	return sessions, nil
}
