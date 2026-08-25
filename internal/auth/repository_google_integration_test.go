package auth

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestFindOrCreateGoogleAccountCreatesVerifiedAccounts(t *testing.T) {
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
	repository := NewRepository(pool)
	now := time.Now().UTC().Truncate(time.Microsecond)

	for _, test := range []struct {
		name     string
		audience Audience
		table    string
	}{
		{name: "app", audience: AudienceApp, table: "app_users"},
		{name: "professional", audience: AudienceProfessional, table: "professional_users"},
	} {
		t.Run(test.name, func(t *testing.T) {
			identityID := uuid.NewString()
			account, created, err := repository.FindOrCreateGoogleAccount(
				ctx,
				test.audience,
				GoogleIdentity{
					Subject: identityID, Email: identityID + "@gmail.com",
					DisplayName: "Google Test", AuthoritativeEmail: true,
				},
				"Google Test",
				"integration-password-hash",
				now,
			)
			if err != nil {
				t.Fatalf("FindOrCreateGoogleAccount() error = %v", err)
			}
			if !created || account.Status != "active" || account.EmailVerifiedAt == nil {
				t.Fatalf("FindOrCreateGoogleAccount() = %#v, created = %v", account, created)
			}
			if _, err := pool.Exec(ctx, "DELETE FROM "+test.table+" WHERE id = $1", account.ID); err != nil {
				t.Fatalf("cleanup Google account: %v", err)
			}
		})
	}
}
