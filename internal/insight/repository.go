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
	ConversationID    string
	Role              string
	ContentCiphertext []byte
	CreatedAt         time.Time
}

type storedSummary struct {
	ID                        string
	ConnectionID              string
	SchemaVersion             string
	TitleCiphertext           []byte
	PeriodStart               time.Time
	PeriodEnd                 time.Time
	CoverageConversationCount int
	CoverageUserMessageCount  int
	CoverageActiveDayCount    int
	CoverageCompleteness      string
	CoverageNoteCiphertext    []byte
	SummaryCiphertext         []byte
	LimitationsCiphertext     []byte
	Provider                  string
	Model                     string
	PromptVersion             string
	GraphVersion              string
	ReviewStatus              string
	ReviewedAt                *time.Time
	CreatedAt                 time.Time
}

type storedItem struct {
	ID                    string
	Kind                  string
	TitleCiphertext       []byte
	DescriptionCiphertext []byte
	ImpactCiphertext      []byte
	EvidenceStrength      string
	OccurredAt            *time.Time
	LimitationsCiphertext []byte
	Included              bool
}

type storedTimelineEntry struct {
	ID                    string
	DescriptionCiphertext []byte
	OccurredAt            *time.Time
}

type storedJob struct {
	ID                 string
	ConnectionID       string
	ProfessionalUserID string
	PeriodStart        time.Time
	PeriodEnd          time.Time
	Status             string
	AttemptCount       int
	CompletedAt        *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type itemWrite struct {
	Kind                  string
	TitleCiphertext       []byte
	DescriptionCiphertext []byte
	ImpactCiphertext      []byte
	EvidenceStrength      string
	OccurredAt            *time.Time
	LimitationsCiphertext []byte
	SourceMessageIDs      []string
}

type timelineWrite struct {
	DescriptionCiphertext []byte
	OccurredAt            *time.Time
	SourceMessageIDs      []string
}

type reportWrite struct {
	SchemaVersion             string
	TitleCiphertext           []byte
	CoverageConversationCount int
	CoverageUserMessageCount  int
	CoverageActiveDayCount    int
	CoverageCompleteness      string
	CoverageNoteCiphertext    []byte
	SummaryCiphertext         []byte
	LimitationsCiphertext     []byte
	Provider                  string
	Model                     string
	PromptVersion             string
	GraphVersion              string
	Timeline                  []timelineWrite
	Items                     []itemWrite
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
		VALUES ($1, $2, $3, $4, 'queued', $5, $5)
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

func (r *Repository) GetJob(
	ctx context.Context,
	professionalUserID string,
	jobID string,
) (storedJob, error) {
	job, err := scanStoredJob(r.pool.QueryRow(ctx, `
		SELECT id::text, connection_id::text, requested_by_professional_user_id::text,
		       period_start, period_end, status, attempt_count, completed_at,
		       created_at, updated_at
		FROM context_processing_jobs
		WHERE id = $1 AND requested_by_professional_user_id = $2
	`, jobID, professionalUserID))
	if errors.Is(err, pgx.ErrNoRows) {
		return storedJob{}, ErrNotFound
	}
	if err != nil {
		return storedJob{}, fmt.Errorf("get context processing job: %w", err)
	}
	return job, nil
}

func (r *Repository) ClaimNextJob(
	ctx context.Context,
	now time.Time,
	staleBefore time.Time,
) (storedJob, bool, error) {
	job, err := scanStoredJob(r.pool.QueryRow(ctx, `
		WITH candidate AS (
			SELECT id
			FROM context_processing_jobs
			WHERE (
				status = 'queued' AND available_at <= $1
			) OR (
				status = 'processing' AND claimed_at < $2
			)
			ORDER BY available_at, created_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE context_processing_jobs AS job
		SET status = 'processing', claimed_at = $1,
		    attempt_count = attempt_count + 1, updated_at = $1
		FROM candidate
		WHERE job.id = candidate.id
		RETURNING job.id::text, job.connection_id::text,
		          job.requested_by_professional_user_id::text,
		          job.period_start, job.period_end, job.status, job.attempt_count,
		          job.completed_at, job.created_at, job.updated_at
	`, now, staleBefore))
	if errors.Is(err, pgx.ErrNoRows) {
		return storedJob{}, false, nil
	}
	if err != nil {
		return storedJob{}, false, fmt.Errorf("claim context processing job: %w", err)
	}
	return job, true, nil
}

func (r *Repository) RetryJob(
	ctx context.Context,
	jobID string,
	failureCode string,
	availableAt time.Time,
	now time.Time,
) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE context_processing_jobs
		SET status = 'queued', failure_code = $2, available_at = $3,
		    claimed_at = NULL, updated_at = $4
		WHERE id = $1 AND status = 'processing'
	`, jobID, failureCode, availableAt, now)
	if err != nil {
		return fmt.Errorf("retry context processing job: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}

func (r *Repository) LoadSourceMessages(
	ctx context.Context,
	appUserID string,
	periodStart time.Time,
	periodEnd time.Time,
	limit int,
) ([]storedSourceMessage, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT message.id::text, conversation.id::text, message.role,
		       message.content_ciphertext, message.created_at
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
			&message.ID, &message.ConversationID, &message.Role,
			&message.ContentCiphertext, &message.CreatedAt,
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
		SET status = 'failed', failure_code = $2, claimed_at = NULL, updated_at = $3
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
	report reportWrite,
	now time.Time,
) (string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin context completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	result, err := tx.Exec(ctx, `
		UPDATE context_processing_jobs
		SET status = 'completed', failure_code = NULL, completed_at = $2,
		    claimed_at = NULL, updated_at = $2
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
			connection_id, processing_job_id, schema_version, title_ciphertext,
			period_start, period_end, coverage_conversation_count,
			coverage_user_message_count, coverage_active_day_count,
			coverage_completeness, coverage_note_ciphertext, summary_ciphertext,
			limitations_ciphertext, provider, model, prompt_version, graph_version,
			review_status, created_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
			$14, $15, $16, $17, 'pending_review', $18
		)
		RETURNING id::text
	`, connectionID, jobID, report.SchemaVersion, report.TitleCiphertext,
		periodStart, periodEnd, report.CoverageConversationCount,
		report.CoverageUserMessageCount, report.CoverageActiveDayCount,
		report.CoverageCompleteness, report.CoverageNoteCiphertext,
		report.SummaryCiphertext, report.LimitationsCiphertext, report.Provider,
		report.Model, report.PromptVersion, report.GraphVersion, now).Scan(&summaryID)
	if err != nil {
		return "", fmt.Errorf("insert context summary: %w", err)
	}

	for _, entry := range report.Timeline {
		var entryID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO context_timeline_entries (
				context_summary_id, description_ciphertext, occurred_at, created_at
			)
			VALUES ($1, $2, $3, $4)
			RETURNING id::text
		`, summaryID, entry.DescriptionCiphertext, entry.OccurredAt, now).Scan(&entryID); err != nil {
			return "", fmt.Errorf("insert context timeline entry: %w", err)
		}
		for _, messageID := range entry.SourceMessageIDs {
			if _, err := tx.Exec(ctx, `
				INSERT INTO context_timeline_sources (
					context_timeline_entry_id, chat_message_id
				)
				VALUES ($1, $2)
			`, entryID, messageID); err != nil {
				return "", fmt.Errorf("insert context timeline source: %w", err)
			}
		}
	}

	for _, item := range report.Items {
		var itemID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO context_items (
				context_summary_id, kind, title_ciphertext, description_ciphertext,
				impact_ciphertext, evidence_strength, occurred_at,
				limitations_ciphertext, created_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			RETURNING id::text
		`, summaryID, item.Kind, item.TitleCiphertext, item.DescriptionCiphertext,
			item.ImpactCiphertext, item.EvidenceStrength, item.OccurredAt,
			item.LimitationsCiphertext, now).Scan(&itemID); err != nil {
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

func scanStoredJob(row rowScanner) (storedJob, error) {
	var job storedJob
	err := row.Scan(
		&job.ID, &job.ConnectionID, &job.ProfessionalUserID,
		&job.PeriodStart, &job.PeriodEnd, &job.Status, &job.AttemptCount,
		&job.CompletedAt, &job.CreatedAt, &job.UpdatedAt,
	)
	return job, err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (r *Repository) ListSummaries(
	ctx context.Context,
	connectionID string,
	limit int,
) ([]storedSummary, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, connection_id::text, schema_version, title_ciphertext,
		       period_start, period_end, coverage_conversation_count,
		       coverage_user_message_count, coverage_active_day_count,
		       coverage_completeness, coverage_note_ciphertext, summary_ciphertext,
		       limitations_ciphertext, provider, model, prompt_version, graph_version,
		       review_status, reviewed_at, created_at
		FROM context_summaries
		WHERE connection_id = $1 AND review_status = 'approved'
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
			&summary.ID, &summary.ConnectionID, &summary.SchemaVersion,
			&summary.TitleCiphertext, &summary.PeriodStart, &summary.PeriodEnd,
			&summary.CoverageConversationCount, &summary.CoverageUserMessageCount,
			&summary.CoverageActiveDayCount, &summary.CoverageCompleteness,
			&summary.CoverageNoteCiphertext, &summary.SummaryCiphertext,
			&summary.LimitationsCiphertext, &summary.Provider, &summary.Model,
			&summary.PromptVersion, &summary.GraphVersion, &summary.ReviewStatus,
			&summary.ReviewedAt, &summary.CreatedAt,
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

func (r *Repository) ListSummariesForApp(
	ctx context.Context,
	appUserID string,
	limit int,
) ([]storedSummary, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT summary.id::text, summary.connection_id::text, summary.schema_version,
		       summary.title_ciphertext, summary.period_start, summary.period_end,
		       summary.coverage_conversation_count, summary.coverage_user_message_count,
		       summary.coverage_active_day_count, summary.coverage_completeness,
		       summary.coverage_note_ciphertext, summary.summary_ciphertext,
		       summary.limitations_ciphertext, summary.provider, summary.model,
		       summary.prompt_version, summary.graph_version, summary.review_status,
		       summary.reviewed_at, summary.created_at
		FROM context_summaries AS summary
		JOIN professional_patient_connections AS connection
		  ON connection.id = summary.connection_id
		WHERE connection.app_user_id = $1 AND connection.status = 'active'
		ORDER BY summary.period_end DESC
		LIMIT $2
	`, appUserID, limit)
	if err != nil {
		return nil, fmt.Errorf("query app context summaries: %w", err)
	}
	defer rows.Close()
	summaries := make([]storedSummary, 0)
	for rows.Next() {
		var summary storedSummary
		if err := scanStoredSummary(rows, &summary); err != nil {
			return nil, fmt.Errorf("scan app context summary: %w", err)
		}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate app context summaries: %w", err)
	}
	return summaries, nil
}

func (r *Repository) ReviewSummary(
	ctx context.Context,
	appUserID string,
	summaryID string,
	decision string,
	excludedItemIDs []string,
	excludedTimelineEntryIDs []string,
	now time.Time,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin context review: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var currentStatus string
	err = tx.QueryRow(ctx, `
		SELECT summary.review_status
		FROM context_summaries AS summary
		JOIN professional_patient_connections AS connection
		  ON connection.id = summary.connection_id
		WHERE summary.id = $1 AND connection.app_user_id = $2
		FOR UPDATE OF summary
	`, summaryID, appUserID).Scan(&currentStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock context review: %w", err)
	}
	if currentStatus != "pending_review" {
		return ErrConflict
	}

	if decision == "approved" && len(excludedItemIDs) > 0 {
		result, err := tx.Exec(ctx, `
			UPDATE context_items
			SET included = false
			WHERE context_summary_id = $1 AND id = ANY($2::uuid[])
		`, summaryID, excludedItemIDs)
		if err != nil {
			return fmt.Errorf("exclude context review items: %w", err)
		}
		if result.RowsAffected() != int64(len(excludedItemIDs)) {
			return ErrInvalidInput
		}
	}
	if decision == "approved" && len(excludedTimelineEntryIDs) > 0 {
		result, err := tx.Exec(ctx, `
			UPDATE context_timeline_entries
			SET included = false
			WHERE context_summary_id = $1 AND id = ANY($2::uuid[])
		`, summaryID, excludedTimelineEntryIDs)
		if err != nil {
			return fmt.Errorf("exclude context review timeline entries: %w", err)
		}
		if result.RowsAffected() != int64(len(excludedTimelineEntryIDs)) {
			return ErrInvalidInput
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE context_summaries
		SET review_status = $2, reviewed_at = $3
		WHERE id = $1
	`, summaryID, decision, now); err != nil {
		return fmt.Errorf("complete context review: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit context review: %w", err)
	}
	return nil
}

func scanStoredSummary(row rowScanner, summary *storedSummary) error {
	return row.Scan(
		&summary.ID, &summary.ConnectionID, &summary.SchemaVersion,
		&summary.TitleCiphertext, &summary.PeriodStart, &summary.PeriodEnd,
		&summary.CoverageConversationCount, &summary.CoverageUserMessageCount,
		&summary.CoverageActiveDayCount, &summary.CoverageCompleteness,
		&summary.CoverageNoteCiphertext, &summary.SummaryCiphertext,
		&summary.LimitationsCiphertext, &summary.Provider, &summary.Model,
		&summary.PromptVersion, &summary.GraphVersion, &summary.ReviewStatus,
		&summary.ReviewedAt, &summary.CreatedAt,
	)
}

func (r *Repository) ListItems(ctx context.Context, summaryID string) ([]storedItem, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, kind, title_ciphertext, description_ciphertext,
		       impact_ciphertext, evidence_strength, occurred_at,
		       limitations_ciphertext, included
		FROM context_items
		WHERE context_summary_id = $1 AND included = true
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
			&item.ID, &item.Kind, &item.TitleCiphertext, &item.DescriptionCiphertext,
			&item.ImpactCiphertext, &item.EvidenceStrength, &item.OccurredAt,
			&item.LimitationsCiphertext, &item.Included,
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

func (r *Repository) ListTimeline(
	ctx context.Context,
	summaryID string,
) ([]storedTimelineEntry, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, description_ciphertext, occurred_at
		FROM context_timeline_entries
		WHERE context_summary_id = $1 AND included = true
		ORDER BY occurred_at NULLS LAST, created_at, id
	`, summaryID)
	if err != nil {
		return nil, fmt.Errorf("query context timeline: %w", err)
	}
	defer rows.Close()
	entries := make([]storedTimelineEntry, 0)
	for rows.Next() {
		var entry storedTimelineEntry
		if err := rows.Scan(&entry.ID, &entry.DescriptionCiphertext, &entry.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan context timeline: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate context timeline: %w", err)
	}
	return entries, nil
}
