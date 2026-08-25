package auth

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepositoryUpdatesAccountAndKeepsOnlyCurrentSession(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()

	accountID := uuid.NewString()
	currentSessionID := uuid.NewString()
	otherSessionID := uuid.NewString()
	oldPasswordHash := "test-old-password-hash"
	newPasswordHash := "test-new-password-hash"
	now := time.Now().UTC()

	if _, err := pool.Exec(ctx, `
		INSERT INTO app_users (
			id, email, password_hash, display_name, status, email_verified_at
		) VALUES ($1, $2, $3, 'Old Name', 'active', $4)
	`, accountID, accountID+"@example.com", oldPasswordHash, now); err != nil {
		t.Fatalf("insert app account: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM auth_sessions WHERE app_user_id = $1`, accountID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM app_users WHERE id = $1`, accountID)
	})

	for index, sessionID := range []string{currentSessionID, otherSessionID} {
		refreshHash := strings.Repeat(string(rune('a'+index)), 64)
		if _, err := pool.Exec(ctx, `
			INSERT INTO auth_sessions (
				id, audience, app_user_id, refresh_token_hash, expires_at
			) VALUES ($1, 'app', $2, $3, $4)
		`, sessionID, accountID, refreshHash, now.Add(time.Hour)); err != nil {
			t.Fatalf("insert auth session: %v", err)
		}
	}

	repository := NewRepository(pool)
	if err := repository.UpdateDisplayName(ctx, AudienceApp, accountID, "New Name", now); err != nil {
		t.Fatalf("UpdateDisplayName() error = %v", err)
	}
	account, err := repository.FindAccountByID(ctx, AudienceApp, accountID)
	if err != nil {
		t.Fatalf("FindAccountByID() error = %v", err)
	}
	if account.DisplayName != "New Name" || account.Plan != "free" || account.CreatedAt.IsZero() {
		t.Fatalf("FindAccountByID() = %#v", account)
	}

	principal := Principal{
		AccountID: accountID, SessionID: currentSessionID, Audience: AudienceApp,
	}
	if err := repository.ChangePassword(
		ctx, principal, oldPasswordHash, newPasswordHash, now.Add(time.Minute),
	); err != nil {
		t.Fatalf("ChangePassword() error = %v", err)
	}
	account, err = repository.FindAccountByID(ctx, AudienceApp, accountID)
	if err != nil || account.PasswordHash != newPasswordHash {
		t.Fatalf("changed account = %#v, error = %v", account, err)
	}

	sessions, err := repository.ListSessions(ctx, principal, now)
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != currentSessionID || !sessions[0].CurrentSession {
		t.Fatalf("ListSessions() = %#v, want only current session", sessions)
	}
}
