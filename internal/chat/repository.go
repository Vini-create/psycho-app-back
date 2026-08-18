package chat

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

type storedConversation struct {
	ID              string
	TitleCiphertext []byte
	Status          string
	LastMessageAt   *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type storedMessage struct {
	ID                 string
	ConversationID     string
	Sequence           int64
	Role               string
	ContentCiphertext  []byte
	InReplyToMessageID *string
	GenerationStatus   string
	AIProvider         *string
	AIModel            *string
	PromptVersion      *string
	FailureCode        *string
	CreatedAt          time.Time
}

func (r *Repository) GrantConsents(
	ctx context.Context,
	appUserID string,
	consentTypes []string,
	policyVersion string,
	client ClientInfo,
	now time.Time,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin consent transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, consentType := range consentTypes {
		if _, err := tx.Exec(ctx, `
			UPDATE app_consents
			SET revoked_at = $4
			WHERE app_user_id = $1
			  AND consent_type = $2
			  AND policy_version <> $3
			  AND revoked_at IS NULL
		`, appUserID, consentType, policyVersion, now); err != nil {
			return fmt.Errorf("revoke superseded consent: %w", err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO app_consents (
				app_user_id, consent_type, policy_version, granted_at, ip_address, user_agent
			)
			VALUES ($1, $2, $3, $4, NULLIF($5, '')::inet, NULLIF($6, ''))
			ON CONFLICT (app_user_id, consent_type) WHERE revoked_at IS NULL
			DO NOTHING
		`, appUserID, consentType, policyVersion, now, client.IPAddress, client.UserAgent); err != nil {
			return fmt.Errorf("grant app consent: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit consent transaction: %w", err)
	}
	return nil
}

func (r *Repository) ListActiveConsents(ctx context.Context, appUserID string) ([]Consent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT consent_type, policy_version, granted_at
		FROM app_consents
		WHERE app_user_id = $1 AND revoked_at IS NULL
		ORDER BY consent_type
	`, appUserID)
	if err != nil {
		return nil, fmt.Errorf("query app consents: %w", err)
	}
	defer rows.Close()

	consents := make([]Consent, 0, 3)
	for rows.Next() {
		var consent Consent
		if err := rows.Scan(&consent.Type, &consent.PolicyVersion, &consent.GrantedAt); err != nil {
			return nil, fmt.Errorf("scan app consent: %w", err)
		}
		consents = append(consents, consent)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate app consents: %w", err)
	}
	return consents, nil
}

func (r *Repository) RevokeConsent(
	ctx context.Context,
	appUserID string,
	consentType string,
	now time.Time,
) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE app_consents
		SET revoked_at = $3
		WHERE app_user_id = $1 AND consent_type = $2 AND revoked_at IS NULL
	`, appUserID, consentType, now)
	if err != nil {
		return fmt.Errorf("revoke app consent: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) HasCurrentConsent(
	ctx context.Context,
	appUserID string,
	consentType string,
	policyVersion string,
) (bool, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM app_consents
			WHERE app_user_id = $1
			  AND consent_type = $2
			  AND policy_version = $3
			  AND revoked_at IS NULL
		)
	`, appUserID, consentType, policyVersion).Scan(&exists); err != nil {
		return false, fmt.Errorf("check app consent: %w", err)
	}
	return exists, nil
}

func (r *Repository) CreateConversation(
	ctx context.Context,
	appUserID string,
	titleCiphertext []byte,
) (storedConversation, error) {
	var conversation storedConversation
	err := r.pool.QueryRow(ctx, `
		INSERT INTO chat_conversations (app_user_id, title_ciphertext)
		VALUES ($1, $2)
		RETURNING id::text, title_ciphertext, status, last_message_at, created_at, updated_at
	`, appUserID, titleCiphertext).Scan(
		&conversation.ID,
		&conversation.TitleCiphertext,
		&conversation.Status,
		&conversation.LastMessageAt,
		&conversation.CreatedAt,
		&conversation.UpdatedAt,
	)
	if err != nil {
		return storedConversation{}, fmt.Errorf("create conversation: %w", err)
	}
	return conversation, nil
}

func (r *Repository) ListConversations(
	ctx context.Context,
	appUserID string,
	limit int,
) ([]storedConversation, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, title_ciphertext, status, last_message_at, created_at, updated_at
		FROM chat_conversations
		WHERE app_user_id = $1
		ORDER BY COALESCE(last_message_at, created_at) DESC, id DESC
		LIMIT $2
	`, appUserID, limit)
	if err != nil {
		return nil, fmt.Errorf("query conversations: %w", err)
	}
	defer rows.Close()

	conversations := make([]storedConversation, 0, limit)
	for rows.Next() {
		var conversation storedConversation
		if err := rows.Scan(
			&conversation.ID,
			&conversation.TitleCiphertext,
			&conversation.Status,
			&conversation.LastMessageAt,
			&conversation.CreatedAt,
			&conversation.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan conversation: %w", err)
		}
		conversations = append(conversations, conversation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conversations: %w", err)
	}
	return conversations, nil
}

func (r *Repository) ArchiveConversation(
	ctx context.Context,
	appUserID string,
	conversationID string,
	now time.Time,
) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE chat_conversations
		SET status = 'archived', archived_at = $3, updated_at = $3
		WHERE id = $1 AND app_user_id = $2 AND status = 'active'
	`, conversationID, appUserID, now)
	if err != nil {
		return fmt.Errorf("archive conversation: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) AppendUserMessage(
	ctx context.Context,
	appUserID string,
	conversationID string,
	contentCiphertext []byte,
	requestHash string,
	now time.Time,
) (storedMessage, bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return storedMessage{}, false, fmt.Errorf("begin user message transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var nextSequence int64
	var status string
	if err := tx.QueryRow(ctx, `
		SELECT next_sequence, status
		FROM chat_conversations
		WHERE id = $1 AND app_user_id = $2
		FOR UPDATE
	`, conversationID, appUserID).Scan(&nextSequence, &status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return storedMessage{}, false, ErrNotFound
		}
		return storedMessage{}, false, fmt.Errorf("lock conversation: %w", err)
	}
	if status != "active" {
		return storedMessage{}, false, ErrConflict
	}

	existing, err := scanStoredMessage(tx.QueryRow(ctx, `
		SELECT id::text, conversation_id::text, sequence, role, content_ciphertext,
		       in_reply_to_message_id::text, generation_status, ai_provider, ai_model,
		       prompt_version, failure_code, created_at
		FROM chat_messages
		WHERE conversation_id = $1 AND client_request_hash = $2 AND role = 'user'
	`, conversationID, requestHash))
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return storedMessage{}, false, fmt.Errorf("commit duplicate message lookup: %w", err)
		}
		return existing, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return storedMessage{}, false, fmt.Errorf("find idempotent message: %w", err)
	}

	message, err := scanStoredMessage(tx.QueryRow(ctx, `
		INSERT INTO chat_messages (
			conversation_id, sequence, role, content_ciphertext,
			client_request_hash, generation_status, generation_attempted_at, created_at
		)
		VALUES ($1, $2, 'user', $3, $4, 'pending', $5, $5)
		RETURNING id::text, conversation_id::text, sequence, role, content_ciphertext,
		          in_reply_to_message_id::text, generation_status, ai_provider, ai_model,
		          prompt_version, failure_code, created_at
	`, conversationID, nextSequence, contentCiphertext, requestHash, now))
	if err != nil {
		return storedMessage{}, false, fmt.Errorf("insert user message: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE chat_conversations
		SET next_sequence = $3, last_message_at = $4, updated_at = $4
		WHERE id = $1 AND app_user_id = $2
	`, conversationID, appUserID, nextSequence+1, now); err != nil {
		return storedMessage{}, false, fmt.Errorf("advance conversation after user message: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return storedMessage{}, false, fmt.Errorf("commit user message: %w", err)
	}
	return message, true, nil
}

func (r *Repository) ListMessages(
	ctx context.Context,
	appUserID string,
	conversationID string,
	beforeSequence int64,
	limit int,
) ([]storedMessage, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT message.id::text, message.conversation_id::text, message.sequence,
		       message.role, message.content_ciphertext, message.in_reply_to_message_id::text,
		       message.generation_status, message.ai_provider, message.ai_model,
		       message.prompt_version, message.failure_code, message.created_at
		FROM chat_messages AS message
		JOIN chat_conversations AS conversation ON conversation.id = message.conversation_id
		WHERE message.conversation_id = $1
		  AND conversation.app_user_id = $2
		  AND ($3::bigint = 0 OR message.sequence < $3)
		ORDER BY message.sequence DESC
		LIMIT $4
	`, conversationID, appUserID, beforeSequence, limit)
	if err != nil {
		return nil, fmt.Errorf("query conversation messages: %w", err)
	}
	defer rows.Close()

	messages := make([]storedMessage, 0, limit)
	for rows.Next() {
		message, err := scanStoredMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("scan conversation message: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conversation messages: %w", err)
	}
	if len(messages) == 0 {
		var exists bool
		if err := r.pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM chat_conversations WHERE id = $1 AND app_user_id = $2
			)
		`, conversationID, appUserID).Scan(&exists); err != nil {
			return nil, fmt.Errorf("check conversation ownership: %w", err)
		}
		if !exists {
			return nil, ErrNotFound
		}
	}
	slices.Reverse(messages)
	return messages, nil
}

func (r *Repository) FindReply(
	ctx context.Context,
	appUserID string,
	userMessageID string,
) (storedMessage, error) {
	message, err := scanStoredMessage(r.pool.QueryRow(ctx, `
		SELECT reply.id::text, reply.conversation_id::text, reply.sequence, reply.role,
		       reply.content_ciphertext, reply.in_reply_to_message_id::text,
		       reply.generation_status, reply.ai_provider, reply.ai_model,
		       reply.prompt_version, reply.failure_code, reply.created_at
		FROM chat_messages AS reply
		JOIN chat_messages AS source ON source.id = reply.in_reply_to_message_id
		JOIN chat_conversations AS conversation ON conversation.id = reply.conversation_id
		WHERE source.id = $1 AND conversation.app_user_id = $2
	`, userMessageID, appUserID))
	if errors.Is(err, pgx.ErrNoRows) {
		return storedMessage{}, ErrNotFound
	}
	if err != nil {
		return storedMessage{}, fmt.Errorf("find assistant reply: %w", err)
	}
	return message, nil
}

func (r *Repository) FindUserMessage(
	ctx context.Context,
	appUserID string,
	messageID string,
) (storedMessage, error) {
	message, err := scanStoredMessage(r.pool.QueryRow(ctx, `
		SELECT message.id::text, message.conversation_id::text, message.sequence,
		       message.role, message.content_ciphertext, message.in_reply_to_message_id::text,
		       message.generation_status, message.ai_provider, message.ai_model,
		       message.prompt_version, message.failure_code, message.created_at
		FROM chat_messages AS message
		JOIN chat_conversations AS conversation ON conversation.id = message.conversation_id
		WHERE message.id = $1 AND message.role = 'user' AND conversation.app_user_id = $2
	`, messageID, appUserID))
	if errors.Is(err, pgx.ErrNoRows) {
		return storedMessage{}, ErrNotFound
	}
	if err != nil {
		return storedMessage{}, fmt.Errorf("find user message: %w", err)
	}
	return message, nil
}

func (r *Repository) ClaimGenerationRetry(
	ctx context.Context,
	appUserID string,
	messageID string,
	now time.Time,
) (bool, error) {
	result, err := r.pool.Exec(ctx, `
		UPDATE chat_messages AS message
		SET generation_status = 'pending', failure_code = NULL, generation_attempted_at = $3
		FROM chat_conversations AS conversation
		WHERE message.id = $1
		  AND message.conversation_id = conversation.id
		  AND conversation.app_user_id = $2
		  AND message.role = 'user'
		  AND (
			message.generation_status = 'failed'
			OR (
				message.generation_status = 'pending'
				AND message.generation_attempted_at < $3 - interval '60 seconds'
			)
		  )
	`, messageID, appUserID, now)
	if err != nil {
		return false, fmt.Errorf("claim failed generation: %w", err)
	}
	return result.RowsAffected() == 1, nil
}

func (r *Repository) FailGeneration(
	ctx context.Context,
	appUserID string,
	messageID string,
	failureCode string,
) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE chat_messages AS message
		SET generation_status = 'failed', failure_code = $3
		FROM chat_conversations AS conversation
		WHERE message.id = $1
		  AND message.conversation_id = conversation.id
		  AND conversation.app_user_id = $2
		  AND message.role = 'user'
		  AND message.generation_status = 'pending'
	`, messageID, appUserID, failureCode)
	if err != nil {
		return fmt.Errorf("mark generation as failed: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}

func (r *Repository) CompleteGeneration(
	ctx context.Context,
	appUserID string,
	userMessageID string,
	contentCiphertext []byte,
	status string,
	provider string,
	model string,
	promptVersion string,
	now time.Time,
) (storedMessage, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return storedMessage{}, fmt.Errorf("begin assistant message transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var conversationID string
	var nextSequence int64
	var sourceStatus string
	if err := tx.QueryRow(ctx, `
		SELECT conversation.id::text, conversation.next_sequence, source.generation_status
		FROM chat_messages AS source
		JOIN chat_conversations AS conversation ON conversation.id = source.conversation_id
		WHERE source.id = $1
		  AND source.role = 'user'
		  AND conversation.app_user_id = $2
		FOR UPDATE OF conversation, source
	`, userMessageID, appUserID).Scan(
		&conversationID,
		&nextSequence,
		&sourceStatus,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return storedMessage{}, ErrNotFound
		}
		return storedMessage{}, fmt.Errorf("lock generation source: %w", err)
	}

	existing, err := scanStoredMessage(tx.QueryRow(ctx, `
		SELECT id::text, conversation_id::text, sequence, role, content_ciphertext,
		       in_reply_to_message_id::text, generation_status, ai_provider, ai_model,
		       prompt_version, failure_code, created_at
		FROM chat_messages
		WHERE in_reply_to_message_id = $1
	`, userMessageID))
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return storedMessage{}, fmt.Errorf("commit existing assistant reply: %w", err)
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return storedMessage{}, fmt.Errorf("find existing assistant reply: %w", err)
	}
	if sourceStatus != "pending" {
		return storedMessage{}, ErrConflict
	}

	message, err := scanStoredMessage(tx.QueryRow(ctx, `
		INSERT INTO chat_messages (
			conversation_id, sequence, role, content_ciphertext,
			in_reply_to_message_id, generation_status,
			ai_provider, ai_model, prompt_version, created_at
		)
		VALUES (
			$1, $2, 'assistant', $3, $4, $5,
			NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), $9
		)
		RETURNING id::text, conversation_id::text, sequence, role, content_ciphertext,
		          in_reply_to_message_id::text, generation_status, ai_provider, ai_model,
		          prompt_version, failure_code, created_at
	`,
		conversationID,
		nextSequence,
		contentCiphertext,
		userMessageID,
		status,
		provider,
		model,
		promptVersion,
		now,
	))
	if err != nil {
		return storedMessage{}, fmt.Errorf("insert assistant message: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE chat_messages
		SET generation_status = $2, failure_code = NULL
		WHERE id = $1
	`, userMessageID, status); err != nil {
		return storedMessage{}, fmt.Errorf("complete source generation: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE chat_conversations
		SET next_sequence = $2, last_message_at = $3, updated_at = $3
		WHERE id = $1
	`, conversationID, nextSequence+1, now); err != nil {
		return storedMessage{}, fmt.Errorf("advance conversation after assistant reply: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return storedMessage{}, fmt.Errorf("commit assistant message: %w", err)
	}
	return message, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanStoredMessage(row rowScanner) (storedMessage, error) {
	var message storedMessage
	err := row.Scan(
		&message.ID,
		&message.ConversationID,
		&message.Sequence,
		&message.Role,
		&message.ContentCiphertext,
		&message.InReplyToMessageID,
		&message.GenerationStatus,
		&message.AIProvider,
		&message.AIModel,
		&message.PromptVersion,
		&message.FailureCode,
		&message.CreatedAt,
	)
	return message, err
}
