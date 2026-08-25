package insight

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Vini-create/psycho-app-back/internal/companion"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepositoryContextLifecycle(t *testing.T) {
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

	professionalID := uuid.NewString()
	appUserID := uuid.NewString()
	organizationID := uuid.NewString()
	membershipID := uuid.NewString()
	connectionID := uuid.NewString()
	conversationID := uuid.NewString()
	messageID := uuid.NewString()
	now := time.Now().UTC()
	periodStart := now.Add(-time.Hour)
	periodEnd := now

	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO professional_users (id, email, password_hash, display_name, status, email_verified_at)
		  VALUES ($1, $2, 'integration-hash', 'Context Professional', 'active', now())`,
			[]any{professionalID, "context-pro-" + professionalID + "@example.com"}},
		{`INSERT INTO app_users (id, email, password_hash, display_name, status, email_verified_at)
		  VALUES ($1, $2, 'integration-hash', 'Context Patient', 'active', now())`,
			[]any{appUserID, "context-app-" + appUserID + "@example.com"}},
		{`INSERT INTO organizations (id, name, kind) VALUES ($1, 'Context Test', 'solo')`, []any{organizationID}},
		{`INSERT INTO subscriptions (organization_id, provider, plan, status)
		  VALUES ($1, 'internal', 'single', 'trialing')`, []any{organizationID}},
		{`INSERT INTO organization_memberships
		  (id, organization_id, professional_user_id, role, status, joined_at)
		  VALUES ($1, $2, $3, 'owner', 'active', $4)`, []any{membershipID, organizationID, professionalID, periodStart.Add(-time.Hour)}},
		{`INSERT INTO professional_patient_connections
		  (id, organization_id, professional_membership_id, app_user_id, requested_by, status,
		   professional_accepted_at, app_user_accepted_at, activated_at)
		  VALUES ($1, $2, $3, $4, 'professional', 'active', $5, $5, $5)`,
			[]any{connectionID, organizationID, membershipID, appUserID, periodStart.Add(-time.Minute)}},
		{`INSERT INTO connection_consents (connection_id, scope, policy_version)
		  VALUES ($1, 'summaries', 'integration-v1'), ($1, 'events', 'integration-v1')`, []any{connectionID}},
		{`INSERT INTO chat_conversations (id, app_user_id, next_sequence, last_message_at)
		  VALUES ($1, $2, 2, $3)`, []any{conversationID, appUserID, periodStart.Add(30 * time.Minute)}},
		{`INSERT INTO chat_messages
		  (id, conversation_id, sequence, role, content_ciphertext, client_request_hash,
		   generation_status, created_at, generation_attempted_at)
		  VALUES ($1, $2, 1, 'user', $3, $4, 'completed', $5, $5)`,
			[]any{messageID, conversationID, []byte("encrypted-source"), strings.Repeat("a", 64), periodStart.Add(30 * time.Minute)}},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("prepare context fixture: %v", err)
		}
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM context_timeline_sources WHERE chat_message_id = $1`, messageID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM context_timeline_entries WHERE context_summary_id IN (SELECT id FROM context_summaries WHERE connection_id = $1)`, connectionID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM context_item_sources WHERE chat_message_id = $1`, messageID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM context_items WHERE context_summary_id IN (SELECT id FROM context_summaries WHERE connection_id = $1)`, connectionID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM context_summaries WHERE connection_id = $1`, connectionID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM context_report_requests WHERE connection_id = $1`, connectionID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM context_processing_jobs WHERE connection_id = $1`, connectionID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM chat_messages WHERE conversation_id = $1`, conversationID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM chat_conversations WHERE id = $1`, conversationID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM connection_consents WHERE connection_id = $1`, connectionID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM professional_patient_connections WHERE id = $1`, connectionID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM organization_memberships WHERE id = $1`, membershipID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM subscriptions WHERE organization_id = $1`, organizationID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM organizations WHERE id = $1`, organizationID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM professional_profiles WHERE professional_user_id = $1`, professionalID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM app_users WHERE id = $1`, appUserID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM professional_users WHERE id = $1`, professionalID)
	})

	repository := NewRepository(pool)
	access, err := repository.ProfessionalConnectionAccess(ctx, professionalID, connectionID, "integration-v1")
	if err != nil || access.AppUserID != appUserID || len(access.Scopes) != 2 || access.ProfileComplete {
		t.Fatalf("ProfessionalConnectionAccess() = %#v, error = %v", access, err)
	}
	messages, err := repository.LoadSourceMessages(ctx, appUserID, periodStart, periodEnd, 10)
	if err != nil || len(messages) != 1 || messages[0].ID != messageID {
		t.Fatalf("LoadSourceMessages() = %#v, error = %v", messages, err)
	}
	service, err := NewService(repository, passthroughCipher{}, integrationCompanion{}, ServiceConfig{
		ConsentPolicyVersion: "integration-v1",
		WorkerLease:          time.Minute,
		MaxAttempts:          3,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if _, err := service.CreateReportRequest(
		ctx, professionalID, connectionID, periodStart, periodEnd,
	); !errors.Is(err, ErrProfileIncomplete) {
		t.Fatalf("CreateReportRequest() incomplete profile error = %v, want ErrProfileIncomplete", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO professional_profiles (
			professional_user_id, profession_type, registration_country_code,
			registration_region, registration_number
		) VALUES ($1, 'psychologist', 'BR', 'SP', $2)
	`, professionalID, "TEST-"+professionalID); err != nil {
		t.Fatalf("complete profile: %v", err)
	}
	reportRequest, err := service.CreateReportRequest(
		ctx, professionalID, connectionID, periodStart, periodEnd,
	)
	if err != nil || reportRequest.Status != "pending" {
		t.Fatalf("CreateReportRequest() = %#v, error = %v", reportRequest, err)
	}
	appRequests, err := service.ListReportRequestsForApp(ctx, appUserID, connectionID)
	if err != nil || len(appRequests) != 1 || appRequests[0].ID != reportRequest.ID {
		t.Fatalf("ListReportRequestsForApp() = %#v, error = %v", appRequests, err)
	}
	result, err := service.SendRequestedReport(ctx, appUserID, reportRequest.ID)
	if err != nil || result.Status != "processing" {
		t.Fatalf("SendRequestedReport() = %#v, error = %v", result, err)
	}
	var jobID string
	if err := pool.QueryRow(ctx, `
		SELECT processing_job_id::text
		FROM context_report_requests
		WHERE id = $1
	`, reportRequest.ID).Scan(&jobID); err != nil {
		t.Fatalf("query authorized job: %v", err)
	}
	processed, err := service.ProcessNext(ctx)
	if err != nil || !processed {
		t.Fatalf("ProcessNext() processed = %v, error = %v", processed, err)
	}
	storedJob, err := repository.GetJob(ctx, professionalID, jobID)
	if err != nil || storedJob.Status != "completed" || storedJob.AttemptCount != 1 {
		t.Fatalf("GetJob() = %#v, error = %v", storedJob, err)
	}
	appRequests, err = service.ListReportRequestsForApp(ctx, appUserID, connectionID)
	if err != nil || len(appRequests) != 1 || appRequests[0].Status != "sent" || appRequests[0].SentAt == nil {
		t.Fatalf("completed ListReportRequestsForApp() = %#v, error = %v", appRequests, err)
	}
	summaries, err := repository.ListSummaries(ctx, connectionID, 10)
	if err != nil || len(summaries) != 1 {
		t.Fatalf("ListSummaries() = %#v, error = %v", summaries, err)
	}
	items, err := repository.ListItems(ctx, summaries[0].ID)
	if err != nil || len(items) != 1 || items[0].Kind != "open_topic" {
		t.Fatalf("ListItems() = %#v, error = %v", items, err)
	}
	timeline, err := repository.ListTimeline(ctx, summaries[0].ID)
	if err != nil || len(timeline) != 1 {
		t.Fatalf("ListTimeline() = %#v, error = %v", timeline, err)
	}
}

type passthroughCipher struct{}

func (passthroughCipher) Encrypt(value []byte) ([]byte, error) { return value, nil }
func (passthroughCipher) Decrypt(value []byte) ([]byte, error) { return value, nil }

type integrationCompanion struct{}

func (integrationCompanion) Respond(
	context.Context,
	companion.Request,
) (companion.Response, error) {
	return companion.Response{}, nil
}

func (integrationCompanion) ProcessContext(
	_ context.Context,
	request companion.ContextRequest,
) (companion.ContextResponse, error) {
	return companion.ContextResponse{
		SchemaVersion: "journey-report-v1",
		Title:         "Integration report",
		Coverage: companion.ReportCoverage{
			ConversationCount: 1, UserMessageCount: 1, ActiveDayCount: 1,
			Completeness: "limited", Note: "Integration coverage",
		},
		Summary: "integration summary",
		Timeline: []companion.TimelineEntry{{
			Description: "integration timeline",
			OccurredAt:  &request.Messages[0].CreatedAt,
			SourceMessageIDs: []string{
				request.Messages[0].ID,
			},
		}},
		Items: []companion.ContextItem{{
			Kind: "open_topic", Title: "Integration topic",
			Description: "integration theme", EvidenceStrength: "explicit_once",
			SourceMessageIDs: []string{request.Messages[0].ID},
		}},
		Provider: "integration", Model: "integration-model",
		PromptVersion: "journey-report-v1", GraphVersion: "journey-report-graph-v1",
	}, nil
}
