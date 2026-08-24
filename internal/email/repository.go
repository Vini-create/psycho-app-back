package email

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

type outboxMessage struct {
	ID              string
	Audience        string
	Kind            string
	Recipient       string
	TokenCiphertext []byte
	AttemptCount    int
}

func (r *Repository) Claim(ctx context.Context, lease time.Duration) (outboxMessage, bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return outboxMessage{}, false, fmt.Errorf("begin email outbox claim: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var message outboxMessage
	err = tx.QueryRow(ctx, `
		WITH candidate AS (
			SELECT id
			FROM auth_email_outbox
			WHERE (status = 'queued' AND available_at <= now())
			   OR (status = 'processing' AND lease_expires_at <= now())
			ORDER BY available_at, created_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE auth_email_outbox AS message
		SET status = 'processing',
			attempt_count = attempt_count + 1,
			lease_expires_at = now() + $1::interval,
			updated_at = now()
		FROM candidate
		WHERE message.id = candidate.id
		RETURNING message.id::text, message.audience, message.kind,
		          message.recipient_email, message.token_ciphertext,
		          message.attempt_count
	`, lease.String()).Scan(
		&message.ID, &message.Audience, &message.Kind, &message.Recipient,
		&message.TokenCiphertext, &message.AttemptCount,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return outboxMessage{}, false, nil
	}
	if err != nil {
		return outboxMessage{}, false, fmt.Errorf("claim email outbox message: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return outboxMessage{}, false, fmt.Errorf("commit email outbox claim: %w", err)
	}
	return message, true, nil
}

func (r *Repository) MarkSent(ctx context.Context, id, providerMessageID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE auth_email_outbox
		SET status = 'sent', provider_message_id = $2, sent_at = now(),
			lease_expires_at = NULL, last_error_code = NULL, updated_at = now()
		WHERE id = $1 AND status = 'processing'
	`, id, providerMessageID)
	if err != nil {
		return fmt.Errorf("mark email sent: %w", err)
	}
	return nil
}

func (r *Repository) MarkFailed(
	ctx context.Context,
	id string,
	attemptCount int,
	maxAttempts int,
	errorCode string,
) error {
	status := "queued"
	delay := time.Duration(1<<min(attemptCount-1, 6)) * time.Minute
	if attemptCount >= maxAttempts {
		status = "failed"
		delay = 0
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE auth_email_outbox
		SET status = $2,
			available_at = now() + $3::interval,
			lease_expires_at = NULL,
			last_error_code = $4,
			updated_at = now()
		WHERE id = $1 AND status = 'processing'
	`, id, status, delay.String(), errorCode)
	if err != nil {
		return fmt.Errorf("mark email delivery failure: %w", err)
	}
	return nil
}
