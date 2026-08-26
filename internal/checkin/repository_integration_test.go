package checkin

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Percorre o ciclo inteiro contra o banco real: criar, enviar, aceitar,
// responder, colher e ler. É aqui que o SQL de autorização é verificado —
// nenhum teste de unidade prova que um JOIN recusa o vínculo errado.
func TestRepositoryCheckinLifecycle(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()

	professionalID := uuid.NewString()
	otherProfessionalID := uuid.NewString()
	appUserID := uuid.NewString()
	organizationID := uuid.NewString()
	membershipID := uuid.NewString()
	connectionID := uuid.NewString()
	now := time.Now().UTC()
	activatedAt := now.AddDate(0, 0, -30)

	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO professional_users (id, email, password_hash, display_name, status, email_verified_at)
		  VALUES ($1, $2, 'integration-hash', 'Check-in Professional', 'active', now())`,
			[]any{professionalID, "checkin-pro-" + professionalID + "@example.com"}},
		{`INSERT INTO professional_users (id, email, password_hash, display_name, status, email_verified_at)
		  VALUES ($1, $2, 'integration-hash', 'Other Professional', 'active', now())`,
			[]any{otherProfessionalID, "checkin-other-" + otherProfessionalID + "@example.com"}},
		{`INSERT INTO app_users (id, email, password_hash, display_name, status, email_verified_at)
		  VALUES ($1, $2, 'integration-hash', 'Check-in Patient', 'active', now())`,
			[]any{appUserID, "checkin-app-" + appUserID + "@example.com"}},
		{`INSERT INTO organizations (id, name, kind) VALUES ($1, 'Check-in Test', 'solo')`,
			[]any{organizationID}},
		{`INSERT INTO subscriptions (organization_id, provider, plan, status)
		  VALUES ($1, 'internal', 'single', 'trialing')`, []any{organizationID}},
		{`INSERT INTO organization_memberships
		  (id, organization_id, professional_user_id, role, status, joined_at)
		  VALUES ($1, $2, $3, 'owner', 'active', $4)`,
			[]any{membershipID, organizationID, professionalID, activatedAt}},
		{`INSERT INTO professional_patient_connections
		  (id, organization_id, professional_membership_id, app_user_id, requested_by, status,
		   professional_accepted_at, app_user_accepted_at, activated_at)
		  VALUES ($1, $2, $3, $4, 'professional', 'active', $5, $5, $5)`,
			[]any{connectionID, organizationID, membershipID, appUserID, activatedAt}},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("prepare check-in fixture: %v", err)
		}
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		for _, query := range []string{
			`DELETE FROM checkin_entry_answers WHERE entry_id IN (
				SELECT entry.id FROM checkin_entries AS entry
				JOIN checkin_assignments AS assignment ON assignment.id = entry.assignment_id
				WHERE assignment.connection_id = $1)`,
			`DELETE FROM checkin_entries WHERE assignment_id IN (
				SELECT id FROM checkin_assignments WHERE connection_id = $1)`,
			`DELETE FROM checkin_collections WHERE connection_id = $1`,
			`DELETE FROM checkin_collection_requests WHERE connection_id = $1`,
			`DELETE FROM checkin_assignments WHERE connection_id = $1`,
			`DELETE FROM checkin_template_options WHERE question_id IN (
				SELECT question.id FROM checkin_template_questions AS question
				JOIN checkin_templates AS template ON template.id = question.template_id
				WHERE template.organization_id = $2)`,
			`DELETE FROM checkin_template_questions WHERE template_id IN (
				SELECT id FROM checkin_templates WHERE organization_id = $2)`,
			`DELETE FROM checkin_templates WHERE organization_id = $2`,
			`DELETE FROM professional_patient_connections WHERE id = $1`,
			`DELETE FROM organization_memberships WHERE organization_id = $2`,
			`DELETE FROM subscriptions WHERE organization_id = $2`,
			`DELETE FROM organizations WHERE id = $2`,
			`DELETE FROM professional_profiles WHERE professional_user_id = $3`,
			`DELETE FROM app_users WHERE id = $4`,
			`DELETE FROM professional_users WHERE id IN ($3, $5)`,
		} {
			_, _ = pool.Exec(
				cleanupCtx, query,
				connectionID, organizationID, professionalID, appUserID, otherProfessionalID,
			)
		}
	})

	repository := NewRepository(pool)
	service, err := NewService(repository, plainCipher{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.now = func() time.Time { return now }

	// Sem perfil completo o profissional não cria nada, mesmo assinante.
	if _, err := service.CreateTemplate(
		ctx, professionalID, validTemplateInput(),
	); !errors.Is(err, ErrProfileIncomplete) {
		t.Fatalf("CreateTemplate() = %v, want ErrProfileIncomplete", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO professional_profiles (
			professional_user_id, profession_type, registration_country_code,
			registration_region, registration_number
		) VALUES ($1, 'psychologist', 'BR', 'SP', $2)
	`, professionalID, "CHECKIN-"+professionalID); err != nil {
		t.Fatalf("complete profile: %v", err)
	}

	input := validTemplateInput()
	input.Questions = append(input.Questions, QuestionInput{
		Prompt: "Como foi o sono?",
		Options: []OptionInput{
			{Label: "Dormi muito mal"},
			{Label: "Dormi mal"},
			{Label: "Dormi razoável"},
			{Label: "Dormi bem"},
			{Label: "Dormi muito bem"},
		},
	})
	template, err := service.CreateTemplate(ctx, professionalID, input)
	if err != nil {
		t.Fatalf("CreateTemplate() error = %v", err)
	}
	if template.Status != "draft" || len(template.Questions) != 2 {
		t.Fatalf("template = %+v, want rascunho com 2 perguntas", template)
	}
	if template.Questions[0].Legend == "" {
		t.Fatal("a legenda da pergunta precisa voltar decifrada: é o que o paciente lê")
	}

	assignment, err := service.AssignTemplate(ctx, professionalID, connectionID, template.ID)
	if err != nil {
		t.Fatalf("AssignTemplate() error = %v", err)
	}
	if assignment.Status != "pending" {
		t.Fatalf("assignment.Status = %q, want pending", assignment.Status)
	}

	// Publicado pelo envio: editar agora reescreveria o que já foi aceito.
	if _, err := service.UpdateTemplate(
		ctx, professionalID, template.ID, input,
	); !errors.Is(err, ErrTemplatePublished) {
		t.Fatalf("UpdateTemplate() = %v, want ErrTemplatePublished", err)
	}

	// Antes do aceite, o check-in não existe na tela inicial do paciente.
	active, err := service.ListAssignmentsForApp(ctx, appUserID, "", []string{"active"}, "")
	if err != nil || len(active) != 0 {
		t.Fatalf("ListAssignmentsForApp(active) = %d, error = %v, want 0", len(active), err)
	}
	pending, err := service.ListAssignmentsForApp(
		ctx, appUserID, connectionID, []string{"pending"}, "",
	)
	if err != nil || len(pending) != 1 {
		t.Fatalf("ListAssignmentsForApp(pending) = %d, error = %v, want 1", len(pending), err)
	}
	if pending[0].ProfessionalDisplayName != "Check-in Professional" {
		t.Fatalf("rótulo = %q, want o nome de quem mandou", pending[0].ProfessionalDisplayName)
	}

	if err := service.RespondToAssignment(ctx, appUserID, assignment.ID, true); err != nil {
		t.Fatalf("RespondToAssignment() error = %v", err)
	}
	if err := service.RespondToAssignment(
		ctx, appUserID, assignment.ID, true,
	); !errors.Is(err, ErrRequestResolved) {
		t.Fatalf("segunda resposta = %v, want ErrRequestResolved", err)
	}

	questions := pending[0].Template.Questions
	answers := []AnswerInput{
		{QuestionID: questions[0].ID, OptionID: questions[0].Options[4].ID},
		{QuestionID: questions[1].ID, OptionID: questions[1].Options[4].ID},
	}

	// Alternativa de outra pergunta é recusada, mesmo sendo um UUID real.
	crossed := []AnswerInput{
		{QuestionID: questions[0].ID, OptionID: questions[1].Options[0].ID},
		{QuestionID: questions[1].ID, OptionID: questions[1].Options[4].ID},
	}
	if _, err := service.SubmitEntry(
		ctx, appUserID, assignment.ID, "", crossed, "",
	); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("SubmitEntry(cruzado) = %v, want ErrInvalidInput", err)
	}

	entry, err := service.SubmitEntry(ctx, appUserID, assignment.ID, "", answers, "key-1")
	if err != nil {
		t.Fatalf("SubmitEntry() error = %v", err)
	}
	if len(entry.Answers) != 2 || entry.EntryDate != formatDate(now) {
		t.Fatalf("entry = %+v, want 2 respostas no dia de hoje", entry)
	}

	// Reenviar o mesmo dia corrige, não duplica.
	if _, err := service.SubmitEntry(
		ctx, appUserID, assignment.ID, "", answers, "key-2",
	); err != nil {
		t.Fatalf("SubmitEntry(correção) error = %v", err)
	}

	active, err = service.ListAssignmentsForApp(ctx, appUserID, "", []string{"active"}, "")
	if err != nil || len(active) != 1 {
		t.Fatalf("ListAssignmentsForApp(active) = %d, error = %v, want 1", len(active), err)
	}
	if !active[0].AnsweredToday || active[0].AnsweredDays != 1 {
		t.Fatalf("estado do dia = %+v, want respondido hoje e 1 dia no total", active[0])
	}

	request, err := service.CreateCollectionRequest(
		ctx, professionalID, connectionID,
		formatDate(now.AddDate(0, 0, -6)), formatDate(now),
	)
	if err != nil {
		t.Fatalf("CreateCollectionRequest() error = %v", err)
	}
	if _, err := service.CreateCollectionRequest(
		ctx, professionalID, connectionID,
		formatDate(now.AddDate(0, 0, -6)), formatDate(now),
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("segundo pedido aberto = %v, want ErrConflict", err)
	}

	// Um ID de check-in que não é do paciente não alcança nada.
	if _, err := service.SendCollection(
		ctx, appUserID, request.ID, []string{uuid.NewString()},
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SendCollection(alheio) = %v, want ErrNotFound", err)
	}

	result, err := service.SendCollection(ctx, appUserID, request.ID, []string{assignment.ID})
	if err != nil {
		t.Fatalf("SendCollection() error = %v", err)
	}
	if result.Status != "sent" || result.CheckinCount != 1 {
		t.Fatalf("result = %+v, want 1 check-in enviado", result)
	}
	if _, err := service.SendCollection(
		ctx, appUserID, request.ID, []string{assignment.ID},
	); !errors.Is(err, ErrRequestResolved) {
		t.Fatalf("segundo envio = %v, want ErrRequestResolved", err)
	}

	collections, err := service.ListCollections(ctx, professionalID, connectionID)
	if err != nil || len(collections) != 1 {
		t.Fatalf("ListCollections() = %d, error = %v, want 1", len(collections), err)
	}
	shared := collections[0].Checkins[0]
	if !shared.AuthoredByYou {
		t.Fatal("o check-in foi criado por quem está lendo: AuthoredByYou deveria ser true")
	}
	if shared.AnsweredDayCount != 1 || shared.PeriodDayCount != 7 {
		t.Fatalf("adesão = %d/%d, want 1/7", shared.AnsweredDayCount, shared.PeriodDayCount)
	}
	if shared.BestDay == nil || shared.BestDay.Date != formatDate(now) {
		t.Fatalf("melhor dia = %+v, want o único dia respondido", shared.BestDay)
	}
	if shared.Questions[0].Normalized != 1 {
		t.Fatalf("pergunta 1 normalizada = %v, want 1 (nota máxima)", shared.Questions[0].Normalized)
	}

	// Revogar tira o check-in do aparelho do paciente na mesma hora.
	if err := service.RevokeAssignment(
		ctx, professionalID, connectionID, assignment.ID,
	); err != nil {
		t.Fatalf("RevokeAssignment() error = %v", err)
	}
	active, err = service.ListAssignmentsForApp(ctx, appUserID, "", []string{"active"}, "")
	if err != nil || len(active) != 0 {
		t.Fatalf("depois da revogação = %d, error = %v, want 0", len(active), err)
	}

	// E o retrato já entregue continua íntegro: era isso que ele autorizou.
	collections, err = service.ListCollections(ctx, professionalID, connectionID)
	if err != nil || len(collections) != 1 {
		t.Fatalf("ListCollections() após revogação = %d, error = %v, want 1", len(collections), err)
	}
}
