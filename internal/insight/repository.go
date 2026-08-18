package insight

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

type connectionAccess struct {
	AppUserID   string
	Scopes      []string
	ActivatedAt time.Time
}

type storedSourceMessage struct {
	ID                string
	Role              string
	ContentCiphertext []byte
	CreatedAt         time.Time
}

type storedSummary struct {
	ID                string
	ConnectionID      string
	PeriodStart       time.Time
	PeriodEnd         time.Time
	SummaryCiphertext []byte
	Provider          string
	Model             string
	PromptVersion     string
	CreatedAt         time.Time
}

type storedItem struct {
	ID                    string
	Kind                  string
	DescriptionCiphertext []byte
	Confidence            *float64
	OccurredAt            *time.Time
}

type itemWrite struct {
	Kind                  string
	DescriptionCiphertext []byte
	Confidence            *float64
	OccurredAt            *time.Time
	SourceMessageIDs      []string
}

func (r *Repository) ProfessionalConnectionAccess(
	ctx context.Context,
	professionalUserID string,
	connectionID string,
	policyVersion string,
) (connectionAccess, error) {
	var access connectionAccess
	err := r.pool.QueryRow(ctx, `
		SELECT connection.app_user_id::text, connection.activated_at,
		       COALESCE(array_agg(consent.scope ORDER BY consent.scope)
		           FILTER (
				WHERE consent.scope IS NOT NULL
				  AND consent.revoked_at IS NULL
				  AND consent.policy_version = $3
			), '{}')
		FROM professional_patient_connections AS connection
		JOIN organization_memberships AS membership
		  ON membership.id = connection.professional_membership_id
		LEFT JOIN connection_consents AS consent ON consent.connection_id = connection.id
		WHERE connection.id = $1
		  AND membership.professional_user_id = $2
		  AND membership.status = 'active'
		  AND connection.status = 'active'
		GROUP BY connection.id
	`, connectionID, professionalUserID, policyVersion).Scan(
		&access.AppUserID, &access.ActivatedAt, &access.Scopes,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return connectionAccess{}, ErrNotFound
	}
	if err != nil {
		return connectionAccess{}, fmt.Errorf("check professional context access: %w", err)
	}
	return access, nil
}

func (r *Repository) CreateJob(
	ctx context.Context,
	connectionID string,
	professionalUserID string,
	periodStart time.Time,
	periodEnd time.Time,
	now time.Time,
) (string, error) {
	var jobID string
	err := r.pool.QueryRow(ctx, `
		INSERT INTO context_processing_jobs (
			connection_id, requested_by_professional_user_id,
			period_start, period_end, status, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, 'processing', $5, $5)
		RETURNING id::text
	`, connectionID, professionalUserID, periodStart, periodEnd, now).Scan(&jobID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return "", ErrConflict
		}
		return "", fmt.Errorf("create context processing job: %w", err)
	}
	return jobID, nil
}

func (r *Repository) LoadSourceMessages(
	ctx context.Context,
	appUserID string,
	periodStart time.Time,
	periodEnd time.Time,
	limit int,
) ([]storedSourceMessage, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT message.id::text, message.role, message.content_ciphertext, message.created_at
		FROM chat_messages AS message
		JOIN chat_conversations AS conversation ON conversation.id = message.conversation_id
		WHERE conversation.app_user_id = $1
		  AND message.created_at >= $2
		  AND message.created_at < $3
		  AND message.generation_status IN ('completed', 'blocked')
		ORDER BY message.created_at, message.sequence
		LIMIT $4
	`, appUserID, periodStart, periodEnd, limit)
	if err != nil {
		return nil, fmt.Errorf("query context source messages: %w", err)
	}
	defer rows.Close()
	messages := make([]storedSourceMessage, 0)
	for rows.Next() {
		var message storedSourceMessage
		if err := rows.Scan(
			&message.ID, &message.Role, &message.ContentCiphertext, &message.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan context source message: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate context source messages: %w", err)
	}
	return messages, nil
}

func (r *Repository) FailJob(
	ctx context.Context,
	jobID string,
	failureCode string,
	now time.Time,
) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE context_processing_jobs
		SET status = 'failed', failure_code = $2, updated_at = $3
		WHERE id = $1 AND status = 'processing'
	`, jobID, failureCode, now)
	if err != nil {
		return fmt.Errorf("fail context processing job: %w", err)
	}
	return nil
}

func (r *Repository) CompleteJob(
	ctx context.Context,
	jobID string,
	connectionID string,
	periodStart time.Time,
	periodEnd time.Time,
	summaryCiphertext []byte,
	provider string,
	model string,
	promptVersion string,
	items []itemWrite,
	now time.Time,
) (string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin context completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	result, err := tx.Exec(ctx, `
		UPDATE context_processing_jobs
		SET status = 'completed', failure_code = NULL, completed_at = $2, updated_at = $2
		WHERE id = $1 AND connection_id = $3 AND status = 'processing'
	`, jobID, now, connectionID)
	if err != nil {
		return "", fmt.Errorf("complete context processing job: %w", err)
	}
	if result.RowsAffected() != 1 {
		return "", ErrConflict
	}

	var summaryID string
	err = tx.QueryRow(ctx, `
		INSERT INTO context_summaries (
			connection_id, processing_job_id, period_start, period_end,
			summary_ciphertext, provider, model, prompt_version, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id::text
	`, connectionID, jobID, periodStart, periodEnd, summaryCiphertext,
		provider, model, promptVersion, now).Scan(&summaryID)
	if err != nil {
		return "", fmt.Errorf("insert context summary: %w", err)
	}

	for _, item := range items {
		var itemID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO context_items (
				context_summary_id, kind, description_ciphertext,
				confidence, occurred_at, created_at
			)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id::text
		`, summaryID, item.Kind, item.DescriptionCiphertext,
			item.Confidence, item.OccurredAt, now).Scan(&itemID); err != nil {
			return "", fmt.Errorf("insert context item: %w", err)
		}
		for _, messageID := range item.SourceMessageIDs {
			if _, err := tx.Exec(ctx, `
				INSERT INTO context_item_sources (context_item_id, chat_message_id)
				VALUES ($1, $2)
			`, itemID, messageID); err != nil {
				return "", fmt.Errorf("insert context item source: %w", err)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit context completion: %w", err)
	}
	return summaryID, nil
}

func (r *Repository) ListSummaries(
	ctx context.Context,
	connectionID string,
	limit int,
) ([]storedSummary, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, connection_id::text, period_start, period_end,
		       summary_ciphertext, provider, model, prompt_version, created_at
		FROM context_summaries
		WHERE connection_id = $1
		ORDER BY period_end DESC
		LIMIT $2
	`, connectionID, limit)
	if err != nil {
		return nil, fmt.Errorf("query context summaries: %w", err)
	}
	defer rows.Close()
	summaries := make([]storedSummary, 0)
	for rows.Next() {
		var summary storedSummary
		if err := rows.Scan(
			&summary.ID, &summary.ConnectionID, &summary.PeriodStart,
			&summary.PeriodEnd, &summary.SummaryCiphertext, &summary.Provider,
			&summary.Model, &summary.PromptVersion, &summary.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan context summary: %w", err)
		}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate context summaries: %w", err)
	}
	return summaries, nil
}

func (r *Repository) ListItems(ctx context.Context, summaryID string) ([]storedItem, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, kind, description_ciphertext, confidence, occurred_at
		FROM context_items
		WHERE context_summary_id = $1
		ORDER BY created_at, id
	`, summaryID)
	if err != nil {
		return nil, fmt.Errorf("query context items: %w", err)
	}
	defer rows.Close()
	items := make([]storedItem, 0)
	for rows.Next() {
		var item storedItem
		if err := rows.Scan(
			&item.ID, &item.Kind, &item.DescriptionCiphertext,
			&item.Confidence, &item.OccurredAt,
		); err != nil {
			return nil, fmt.Errorf("scan context item: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate context items: %w", err)
	}
	return items, nil
}
