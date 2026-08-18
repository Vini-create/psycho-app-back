package care

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCareFlowIntegration(t *testing.T) {
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
	appEmail := "care-app-" + appUserID + "@example.com"
	professionalEmail := "care-pro-" + professionalID + "@example.com"
	if _, err := pool.Exec(ctx, `
		INSERT INTO professional_users (
			id, email, password_hash, display_name, status, email_verified_at
		) VALUES ($1, $2, 'integration-hash', 'Profissional Teste', 'active', now())
	`, professionalID, professionalEmail); err != nil {
		t.Fatalf("insert professional: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO app_users (
			id, email, password_hash, display_name, status, email_verified_at
		) VALUES ($1, $2, 'integration-hash', 'Paciente Teste', 'active', now())
	`, appUserID, appEmail); err != nil {
		t.Fatalf("insert app user: %v", err)
	}

	var organizationID string
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `
			DELETE FROM connection_consents WHERE connection_id IN (
				SELECT id FROM professional_patient_connections WHERE app_user_id = $1
			)
		`, appUserID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM professional_patient_connections WHERE app_user_id = $1`, appUserID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM connection_invitations WHERE target_email = $1`, appEmail)
		if organizationID != "" {
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM subscriptions WHERE organization_id = $1`, organizationID)
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM organization_memberships WHERE organization_id = $1`, organizationID)
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM organizations WHERE id = $1`, organizationID)
		}
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM professional_profiles WHERE professional_user_id = $1`, professionalID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM app_users WHERE id = $1`, appUserID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM professional_users WHERE id = $1`, professionalID)
	})

	service, err := NewService(NewRepository(pool), ServiceConfig{
		InvitationTTL: 7 * 24 * time.Hour, ConsentPolicyVersion: "integration-v1",
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	profile, err := service.UpsertProfessionalProfile(ctx, professionalID, ProfessionalProfileInput{
		ProfessionType: "psychologist", RegistrationCountryCode: "BR",
		RegistrationRegion: "SP", RegistrationNumber: "TEST-" + professionalID,
		Bio:            "Perfil criado pelo teste de integração.",
		Certifications: []string{"Certificação de teste"},
	})
	if err != nil {
		t.Fatalf("UpsertProfessionalProfile() error = %v", err)
	}
	organizationID = profile.OrganizationID

	invitation, err := service.CreateInvitation(ctx, professionalID, appEmail)
	if err != nil {
		t.Fatalf("CreateInvitation() error = %v", err)
	}
	if _, err := service.PreviewInvitation(ctx, invitation.InvitationToken); err != nil {
		t.Fatalf("PreviewInvitation() error = %v", err)
	}
	connectionID, err := service.AcceptInvitation(
		ctx,
		appUserID,
		invitation.InvitationToken,
		[]string{"summaries", "events"},
		ClientInfo{IPAddress: "127.0.0.1", UserAgent: "integration-test"},
	)
	if err != nil {
		t.Fatalf("AcceptInvitation() error = %v", err)
	}

	patients, err := service.ListProfessionalPatients(ctx, professionalID)
	if err != nil || len(patients) != 1 || patients[0].ID != connectionID {
		t.Fatalf("ListProfessionalPatients() = %#v, error = %v", patients, err)
	}
	connections, err := service.ListAppConnections(ctx, appUserID)
	if err != nil || len(connections) != 1 || connections[0].ID != connectionID {
		t.Fatalf("ListAppConnections() = %#v, error = %v", connections, err)
	}
	if err := service.ReplaceConnectionConsents(
		ctx,
		appUserID,
		connectionID,
		[]string{"summaries", "marked_topics"},
		ClientInfo{IPAddress: "127.0.0.1", UserAgent: "integration-test"},
	); err != nil {
		t.Fatalf("ReplaceConnectionConsents() error = %v", err)
	}
	if err := service.EndProfessionalConnection(ctx, professionalID, connectionID); err != nil {
		t.Fatalf("EndProfessionalConnection() error = %v", err)
	}
}
