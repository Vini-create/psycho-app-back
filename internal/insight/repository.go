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
	AppUserID          string
	Scopes             []string
	ActivatedAt        time.Time
	SubscriptionStatus string
	ProfileComplete    bool
}

type storedReportRequest struct {
	ID                      string
	ConnectionID            string
	ProfessionalDisplayName string
	PatientDisplayName      string
	PeriodStart             time.Time
	PeriodEnd               time.Time
	Status                  string
	RequestedAt             time.Time
	SentAt                  *time.Time
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
	EmotionalValence      string
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
	EmotionalValence      string
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
		       COALESCE(subscription.status, 'inactive'),
		       COALESCE(bool_and(
		           profile.professional_user_id IS NOT NULL
		           AND char_length(btrim(profile.registration_country_code)) = 2
		           AND char_length(btrim(profile.registration_region)) > 0
		           AND (
		               profile.profession_type NOT IN (
		                   'psychologist', 'psychiatrist', 'occupational_therapist'
		               )
		               OR char_length(btrim(profile.registration_number)) > 0
		           )
		       ), false),
		       COALESCE(array_agg(consent.scope ORDER BY consent.scope)
		           FILTER (
				WHERE consent.scope IS NOT NULL
				  AND consent.revoked_at IS NULL
				  AND consent.policy_version = $3
			), '{}')
		FROM professional_patient_connections AS connection
		JOIN organization_memberships AS membership
		  ON membership.id = connection.professional_membership_id
		LEFT JOIN subscriptions AS subscription
		  ON subscription.organization_id = connection.organization_id
		LEFT JOIN professional_profiles AS profile
		  ON profile.professional_user_id = membership.professional_user_id
		LEFT JOIN connection_consents AS consent ON consent.connection_id = connection.id
		WHERE connection.id = $1
		  AND membership.professional_user_id = $2
		  AND membership.status = 'active'
		  AND connection.status = 'active'
		GROUP BY connection.id, subscription.status
	`, connectionID, professionalUserID, policyVersion).Scan(
		&access.AppUserID, &access.ActivatedAt, &access.SubscriptionStatus,
		&access.ProfileComplete,
		&access.Scopes,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return connectionAccess{}, ErrNotFound
	}
	if err != nil {
		return connectionAccess{}, fmt.Errorf("check professional context access: %w", err)
	}
	return access, nil
}

func (r *Repository) CreateReportRequest(
	ctx context.Context,
	connectionID string,
	professionalUserID string,
	periodStart time.Time,
	periodEnd time.Time,
	now time.Time,
) (storedReportRequest, error) {
	var request storedReportRequest
	err := r.pool.QueryRow(ctx, `
		INSERT INTO context_report_requests (
			connection_id, requested_by_professional_user_id,
			period_start, period_end, status, requested_at, updated_at
		)
		VALUES ($1, $2, $3, $4, 'pending', $5, $5)
		RETURNING id::text, connection_id::text, period_start, period_end,
		          status, requested_at, sent_at
	`, connectionID, professionalUserID, periodStart, periodEnd, now).Scan(
		&request.ID, &request.ConnectionID, &request.PeriodStart, &request.PeriodEnd,
		&request.Status, &request.RequestedAt, &request.SentAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return storedReportRequest{}, ErrConflict
		}
		return storedReportRequest{}, fmt.Errorf("create context report request: %w", err)
	}
	return request, nil
}

func (r *Repository) ListReportRequestsForProfessional(
	ctx context.Context,
	professionalUserID string,
	connectionID string,
	limit int,
) ([]storedReportRequest, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT request.id::text, request.connection_id::text,
		       professional.display_name, patient.display_name,
		       request.period_start, request.period_end, request.status,
		       request.requested_at, request.sent_at
		FROM context_report_requests AS request
		JOIN professional_patient_connections AS connection
		  ON connection.id = request.connection_id
		JOIN organization_memberships AS membership
		  ON membership.id = connection.professional_membership_id
		JOIN professional_users AS professional
		  ON professional.id = request.requested_by_professional_user_id
		JOIN app_users AS patient ON patient.id = connection.app_user_id
		WHERE request.connection_id = $1
		  AND membership.professional_user_id = $2
		ORDER BY request.requested_at DESC
		LIMIT $3
	`, connectionID, professionalUserID, limit)
	if err != nil {
		return nil, fmt.Errorf("query professional context report requests: %w", err)
	}
	defer rows.Close()
	return scanReportRequests(rows)
}

func (r *Repository) ListReportRequestsForApp(
	ctx context.Context,
	appUserID string,
	connectionID string,
	limit int,
) ([]storedReportRequest, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT request.id::text, request.connection_id::text,
		       professional.display_name, patient.display_name,
		       request.period_start, request.period_end, request.status,
		       request.requested_at, request.sent_at
		FROM context_report_requests AS request
		JOIN professional_patient_connections AS connection
		  ON connection.id = request.connection_id
		JOIN professional_users AS professional
		  ON professional.id = request.requested_by_professional_user_id
		JOIN app_users AS patient ON patient.id = connection.app_user_id
		WHERE request.connection_id = $1 AND connection.app_user_id = $2
		ORDER BY request.requested_at DESC
		LIMIT $3
	`, connectionID, appUserID, limit)
	if err != nil {
		return nil, fmt.Errorf("query app context report requests: %w", err)
	}
	defer rows.Close()
	return scanReportRequests(rows)
}

func (r *Repository) AppOwnsConnection(
	ctx context.Context,
	appUserID string,
	connectionID string,
) error {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT true
		FROM professional_patient_connections
		WHERE id = $1 AND app_user_id = $2
	`, connectionID, appUserID).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("check app connection ownership: %w", err)
	}
	return nil
}

func scanReportRequests(rows pgx.Rows) ([]storedReportRequest, error) {
	requests := make([]storedReportRequest, 0)
	for rows.Next() {
		var request storedReportRequest
		if err := rows.Scan(
			&request.ID, &request.ConnectionID,
			&request.ProfessionalDisplayName, &request.PatientDisplayName,
			&request.PeriodStart, &request.PeriodEnd, &request.Status,
			&request.RequestedAt, &request.SentAt,
		); err != nil {
			return nil, fmt.Errorf("scan context report request: %w", err)
		}
		requests = append(requests, request)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate context report requests: %w", err)
	}
	return requests, nil
}

func (r *Repository) AuthorizeReportRequest(
	ctx context.Context,
	appUserID string,
	requestID string,
	policyVersion string,
	now time.Time,
) (string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin context report authorization: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var connectionID, professionalUserID, status string
	var periodStart, periodEnd time.Time
	err = tx.QueryRow(ctx, `
		SELECT request.connection_id::text,
		       request.requested_by_professional_user_id::text,
		       request.period_start, request.period_end, request.status
		FROM context_report_requests AS request
		JOIN professional_patient_connections AS connection
		  ON connection.id = request.connection_id
		WHERE request.id = $1 AND connection.app_user_id = $2
		FOR UPDATE OF request
	`, requestID, appUserID).Scan(
		&connectionID, &professionalUserID, &periodStart, &periodEnd, &status,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("lock context report request: %w", err)
	}
	if status != "pending" {
		return "", ErrRequestResolved
	}

	var connectionStatus, membershipStatus, subscriptionStatus string
	var activatedAt time.Time
	var consentActive bool
	err = tx.QueryRow(ctx, `
		SELECT connection.status, connection.activated_at, membership.status,
		       COALESCE(subscription.status, 'inactive'),
		       EXISTS (
				SELECT 1 FROM connection_consents AS consent
				WHERE consent.connection_id = connection.id
				  AND consent.scope = 'summaries'
				  AND consent.policy_version = $4
				  AND consent.revoked_at IS NULL
		       )
		FROM professional_patient_connections AS connection
		JOIN organization_memberships AS membership
		  ON membership.id = connection.professional_membership_id
		LEFT JOIN subscriptions AS subscription
		  ON subscription.organization_id = connection.organization_id
		WHERE connection.id = $1
		  AND connection.app_user_id = $2
		  AND membership.professional_user_id = $3
	`, connectionID, appUserID, professionalUserID, policyVersion).Scan(
		&connectionStatus, &activatedAt, &membershipStatus,
		&subscriptionStatus, &consentActive,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("recheck context report authorization: %w", err)
	}
	if connectionStatus != "active" || membershipStatus != "active" || periodStart.Before(activatedAt) {
		return "", ErrForbidden
	}
	if !consentActive {
		return "", ErrForbidden
	}
	if subscriptionStatus != "active" && subscriptionStatus != "trialing" {
		return "", ErrSubscriptionRequired
	}

	var jobID string
	err = tx.QueryRow(ctx, `
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
		return "", fmt.Errorf("create authorized context processing job: %w", err)
	}

	result, err := tx.Exec(ctx, `
		UPDATE context_report_requests
		SET status = 'processing', processing_job_id = $2, updated_at = $3
		WHERE id = $1 AND status = 'pending'
	`, requestID, jobID, now)
	if err != nil {
		return "", fmt.Errorf("mark context report request processing: %w", err)
	}
	if result.RowsAffected() != 1 {
		return "", ErrRequestResolved
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit context report authorization: %w", err)
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
			SELECT job.id
			FROM context_processing_jobs AS job
			JOIN context_report_requests AS request
			  ON request.processing_job_id = job.id
			 AND request.status = 'processing'
			WHERE (
				job.status = 'queued' AND job.available_at <= $1
			) OR (
				job.status = 'processing' AND job.claimed_at < $2
			)
			ORDER BY job.available_at, job.created_at
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
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin failed context job: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		UPDATE context_processing_jobs
		SET status = 'failed', failure_code = $2, claimed_at = NULL, updated_at = $3
		WHERE id = $1 AND status = 'processing'
	`, jobID, failureCode, now); err != nil {
		return fmt.Errorf("fail context processing job: %w", err)
	}
	requestResult, err := tx.Exec(ctx, `
		UPDATE context_report_requests
		SET status = 'failed', updated_at = $2
		WHERE processing_job_id = $1 AND status = 'processing'
	`, jobID, now)
	if err != nil {
		return fmt.Errorf("fail context report request: %w", err)
	}
	if requestResult.RowsAffected() != 1 {
		return ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit failed context job: %w", err)
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
			review_status, reviewed_at, created_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
			$14, $15, $16, $17, 'approved', $18, $18
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
				impact_ciphertext, evidence_strength, emotional_valence, occurred_at,
				limitations_ciphertext, created_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, $9, $10)
			RETURNING id::text
		`, summaryID, item.Kind, item.TitleCiphertext, item.DescriptionCiphertext,
			item.ImpactCiphertext, item.EvidenceStrength, item.EmotionalValence, item.OccurredAt,
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

	requestResult, err := tx.Exec(ctx, `
		UPDATE context_report_requests
		SET status = 'sent', sent_at = $2, updated_at = $2
		WHERE processing_job_id = $1 AND status = 'processing'
	`, jobID, now)
	if err != nil {
		return "", fmt.Errorf("complete context report request: %w", err)
	}
	if requestResult.RowsAffected() != 1 {
		return "", ErrConflict
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
		       impact_ciphertext, evidence_strength, COALESCE(emotional_valence, ''), occurred_at,
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
			&item.ImpactCiphertext, &item.EvidenceStrength, &item.EmotionalValence, &item.OccurredAt,
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
