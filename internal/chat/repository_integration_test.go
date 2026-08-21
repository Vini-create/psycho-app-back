package chat

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepositoryConversationLifecycle(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()

	appUserID := uuid.NewString()
	email := "chat-test-" + appUserID + "@example.com"
	if _, err := pool.Exec(ctx, `
		INSERT INTO app_users (
			id, email, password_hash, display_name, status, email_verified_at
		)
		VALUES ($1, $2, 'integration-test-hash', 'Chat Test', 'active', now())
	`, appUserID, email); err != nil {
		t.Fatalf("insert test app user: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `
			DELETE FROM chat_messages
			WHERE conversation_id IN (
				SELECT id FROM chat_conversations WHERE app_user_id = $1
			)
		`, appUserID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM chat_conversations WHERE app_user_id = $1`, appUserID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM app_consents WHERE app_user_id = $1`, appUserID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM app_users WHERE id = $1`, appUserID)
	})

	repository := NewRepository(pool)
	now := time.Now().UTC()
	if err := repository.GrantConsents(
		ctx,
		appUserID,
		[]string{ConsentTerms, ConsentPrivacy, ConsentAIProcessing},
		"integration-v1",
		ClientInfo{IPAddress: "127.0.0.1", UserAgent: "integration-test"},
		now,
	); err != nil {
		t.Fatalf("GrantConsents() error = %v", err)
	}

	conversation, err := repository.CreateConversation(ctx, appUserID, []byte("encrypted-title"))
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	renamed, err := repository.RenameConversation(
		ctx, appUserID, conversation.ID, []byte("renamed-encrypted-title"), now.Add(time.Second),
	)
	if err != nil {
		t.Fatalf("RenameConversation() error = %v", err)
	}
	if string(renamed.TitleCiphertext) != "renamed-encrypted-title" {
		t.Fatalf("RenameConversation() title = %q", renamed.TitleCiphertext)
	}

	userMessage, created, err := repository.AppendUserMessage(
		ctx,
		appUserID,
		conversation.ID,
		[]byte("encrypted-user-message"),
		strings.Repeat("a", 64),
		now,
	)
	if err != nil || !created {
		t.Fatalf("AppendUserMessage() created = %v, error = %v", created, err)
	}

	duplicate, created, err := repository.AppendUserMessage(
		ctx,
		appUserID,
		conversation.ID,
		[]byte("different-ciphertext-must-not-be-inserted"),
		strings.Repeat("a", 64),
		now,
	)
	if err != nil || created || duplicate.ID != userMessage.ID {
		t.Fatalf("idempotent append created = %v, id = %q, error = %v", created, duplicate.ID, err)
	}

	assistantMessage, err := repository.CompleteGeneration(
		ctx,
		appUserID,
		userMessage.ID,
		[]byte("encrypted-assistant-message"),
		"completed",
		"test-provider",
		"test-model",
		"test-prompt-v1",
		now.Add(time.Second),
	)
	if err != nil {
		t.Fatalf("CompleteGeneration() error = %v", err)
	}
	if assistantMessage.Sequence != userMessage.Sequence+1 {
		t.Fatalf("assistant sequence = %d, user sequence = %d", assistantMessage.Sequence, userMessage.Sequence)
	}

	messages, err := repository.ListMessages(ctx, appUserID, conversation.ID, 0, 50)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 2 || messages[0].Role != "user" || messages[1].Role != "assistant" {
		t.Fatalf("ListMessages() = %#v", messages)
	}

	if err := repository.ArchiveConversation(ctx, appUserID, conversation.ID, now.Add(2*time.Second)); err != nil {
		t.Fatalf("ArchiveConversation() error = %v", err)
	}
	activeConversations, err := repository.ListConversations(ctx, appUserID, 50)
	if err != nil {
		t.Fatalf("ListConversations() after archive error = %v", err)
	}
	if len(activeConversations) != 0 {
		t.Fatalf("ListConversations() returned archived conversation: %#v", activeConversations)
	}
}
