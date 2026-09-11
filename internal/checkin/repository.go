package checkin

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

type professionalContext struct {
	MembershipID       string
	OrganizationID     string
	SubscriptionStatus string
	ProfileComplete    bool
}

type connectionAccess struct {
	AppUserID          string
	MembershipID       string
	OrganizationID     string
	ActivatedAt        time.Time
	SubscriptionStatus string
	ProfileComplete    bool
}

type storedTemplate struct {
	ID               string
	TitleCiphertext  []byte
	LegendCiphertext []byte
	Status           string
	PublishedAt      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type storedQuestion struct {
	ID               string
	TemplateID       string
	Position         int
	PromptCiphertext []byte
	LegendCiphertext []byte
}

type storedOption struct {
	ID              string
	QuestionID      string
	Position        int
	Score           int
	LabelCiphertext []byte
}

type storedAssignment struct {
	ID                      string
	ConnectionID            string
	TemplateID              string
	Status                  string
	ProfessionalDisplayName string
	PatientDisplayName      string
	AuthoredByMembershipID  string
	RequestedAt             time.Time
	RespondedAt             *time.Time
	EndedAt                 *time.Time
}

type storedEntry struct {
	ID           string
	AssignmentID string
	EntryDate    time.Time
	SubmittedAt  time.Time
	UpdatedAt    time.Time
}

type storedAnswer struct {
	EntryID    string
	QuestionID string
	OptionID   string
	Score      int
}

type storedCollectionRequest struct {
	ID                      string
	ConnectionID            string
	ProfessionalDisplayName string
	PatientDisplayName      string
	PeriodStart             time.Time
	PeriodEnd               time.Time
	Status                  string
	RequestedAt             time.Time
	RespondedAt             *time.Time
}

type storedCollection struct {
	ID                string
	ConnectionID      string
	RequestID         string
	PeriodStart       time.Time
	PeriodEnd         time.Time
	PayloadCiphertext []byte
	SharedAt          time.Time
}

type templateWrite struct {
	TitleCiphertext  []byte
	LegendCiphertext []byte
	Questions        []questionWrite
}

type questionWrite struct {
	PromptCiphertext []byte
	LegendCiphertext []byte
	Options          []optionWrite
}

type optionWrite struct {
	LabelCiphertext []byte
	Score           int
}

/* ------------------------------------------------------------- acessos */

// ProfessionalContext resolve o vínculo organizacional de quem chama. Toda
// rota de template depende dele: um template pertence à filiação, não ao
// usuário solto.
func (r *Repository) ProfessionalContext(
	ctx context.Context,
	professionalUserID string,
) (professionalContext, error) {
	var result professionalContext
	err := r.pool.QueryRow(ctx, `
		SELECT membership.id::text, membership.organization_id::text,
		       COALESCE(subscription.status, 'inactive'),
		       COALESCE(
		           profile.professional_user_id IS NOT NULL
		           AND char_length(btrim(profile.registration_country_code)) = 2
		           AND char_length(btrim(profile.registration_region)) > 0
		           AND (
		               profile.profession_type NOT IN (
		                   'psychologist', 'psychiatrist', 'occupational_therapist'
		               )
		               OR char_length(btrim(profile.registration_number)) > 0
		           ),
		           false
		       )
		FROM organization_memberships AS membership
		LEFT JOIN subscriptions AS subscription
		  ON subscription.organization_id = membership.organization_id
		LEFT JOIN professional_profiles AS profile
		  ON profile.professional_user_id = membership.professional_user_id
		WHERE membership.professional_user_id = $1
		  AND membership.status = 'active'
	`, professionalUserID).Scan(
		&result.MembershipID, &result.OrganizationID,
		&result.SubscriptionStatus, &result.ProfileComplete,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return professionalContext{}, ErrNotFound
	}
	if err != nil {
		return professionalContext{}, fmt.Errorf("load professional context: %w", err)
	}
	return result, nil
}

// ProfessionalConnectionAccess é o portão de todas as rotas por paciente:
// vínculo ativo, filiação ativa, assinatura e perfil completo, numa consulta
// só. Nenhum handler decide acesso sem passar por aqui.
func (r *Repository) ProfessionalConnectionAccess(
	ctx context.Context,
	professionalUserID string,
	connectionID string,
) (connectionAccess, error) {
	var access connectionAccess
	err := r.pool.QueryRow(ctx, `
		SELECT connection.app_user_id::text, membership.id::text,
		       connection.organization_id::text, connection.activated_at,
		       COALESCE(subscription.status, 'inactive'),
		       COALESCE(
		           profile.professional_user_id IS NOT NULL
		           AND char_length(btrim(profile.registration_country_code)) = 2
		           AND char_length(btrim(profile.registration_region)) > 0
		           AND (
		               profile.profession_type NOT IN (
		                   'psychologist', 'psychiatrist', 'occupational_therapist'
		               )
		               OR char_length(btrim(profile.registration_number)) > 0
		           ),
		           false
		       )
		FROM professional_patient_connections AS connection
		JOIN organization_memberships AS membership
		  ON membership.id = connection.professional_membership_id
		LEFT JOIN subscriptions AS subscription
		  ON subscription.organization_id = connection.organization_id
		LEFT JOIN professional_profiles AS profile
		  ON profile.professional_user_id = membership.professional_user_id
		WHERE connection.id = $1
		  AND membership.professional_user_id = $2
		  AND membership.status = 'active'
		  AND connection.status = 'active'
	`, connectionID, professionalUserID).Scan(
		&access.AppUserID, &access.MembershipID, &access.OrganizationID,
		&access.ActivatedAt, &access.SubscriptionStatus, &access.ProfileComplete,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return connectionAccess{}, ErrNotFound
	}
	if err != nil {
		return connectionAccess{}, fmt.Errorf("check professional check-in access: %w", err)
	}
	return access, nil
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

/* ----------------------------------------------------------- templates */

func (r *Repository) CreateTemplate(
	ctx context.Context,
	membershipID string,
	organizationID string,
	write templateWrite,
	now time.Time,
) (string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin create check-in template: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var templateID string
	err = tx.QueryRow(ctx, `
		INSERT INTO checkin_templates (
			organization_id, professional_membership_id,
			title_ciphertext, legend_ciphertext, status, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, 'draft', $5, $5)
		RETURNING id::text
	`, organizationID, membershipID, write.TitleCiphertext, write.LegendCiphertext, now).
		Scan(&templateID)
	if err != nil {
		return "", fmt.Errorf("insert check-in template: %w", err)
	}

	if err := insertTemplateContent(ctx, tx, templateID, write.Questions, now); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit create check-in template: %w", err)
	}
	return templateID, nil
}

// ReplaceTemplateContent só existe para rascunho. Um template publicado é
// imutável: já pode haver dias respondidos contra aquele enunciado, e mudá-lo
// reescreveria o significado do histórico.
func (r *Repository) ReplaceTemplateContent(
	ctx context.Context,
	membershipID string,
	templateID string,
	write templateWrite,
	now time.Time,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin update check-in template: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	err = tx.QueryRow(ctx, `
		SELECT status FROM checkin_templates
		WHERE id = $1 AND professional_membership_id = $2
		FOR UPDATE
	`, templateID, membershipID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock check-in template: %w", err)
	}
	if status != "draft" {
		return ErrTemplatePublished
	}

	if _, err := tx.Exec(ctx, `
		UPDATE checkin_templates
		SET title_ciphertext = $2, legend_ciphertext = $3, updated_at = $4
		WHERE id = $1
	`, templateID, write.TitleCiphertext, write.LegendCiphertext, now); err != nil {
		return fmt.Errorf("update check-in template: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM checkin_template_questions WHERE template_id = $1
	`, templateID); err != nil {
		return fmt.Errorf("clear check-in template questions: %w", err)
	}
	if err := insertTemplateContent(ctx, tx, templateID, write.Questions, now); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit update check-in template: %w", err)
	}
	return nil
}

func insertTemplateContent(
	ctx context.Context,
	tx pgx.Tx,
	templateID string,
	questions []questionWrite,
	now time.Time,
) error {
	for questionIndex, question := range questions {
		var questionID string
		err := tx.QueryRow(ctx, `
			INSERT INTO checkin_template_questions (
				template_id, position, prompt_ciphertext, legend_ciphertext, created_at
			)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING id::text
		`, templateID, questionIndex+1, question.PromptCiphertext,
			question.LegendCiphertext, now).Scan(&questionID)
		if err != nil {
			return fmt.Errorf("insert check-in question: %w", err)
		}
		for optionIndex, option := range question.Options {
			if _, err := tx.Exec(ctx, `
				INSERT INTO checkin_template_options (
					question_id, position, label_ciphertext, score, created_at
				)
				VALUES ($1, $2, $3, $4, $5)
			`, questionID, optionIndex+1, option.LabelCiphertext, option.Score, now); err != nil {
				return fmt.Errorf("insert check-in option: %w", err)
			}
		}
	}
	return nil
}

func (r *Repository) ArchiveTemplate(
	ctx context.Context,
	membershipID string,
	templateID string,
	now time.Time,
) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE checkin_templates
		SET status = 'archived', archived_at = $3, updated_at = $3,
		    published_at = COALESCE(published_at, $3)
		WHERE id = $1 AND professional_membership_id = $2 AND status <> 'archived'
	`, templateID, membershipID, now)
	if err != nil {
		return fmt.Errorf("archive check-in template: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) ListTemplates(
	ctx context.Context,
	membershipID string,
	limit int,
) ([]storedTemplate, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, title_ciphertext, legend_ciphertext, status,
		       published_at, created_at, updated_at
		FROM checkin_templates
		WHERE professional_membership_id = $1 AND status <> 'archived'
		ORDER BY created_at DESC
		LIMIT $2
	`, membershipID, limit)
	if err != nil {
		return nil, fmt.Errorf("query check-in templates: %w", err)
	}
	defer rows.Close()
	return scanTemplates(rows)
}

func (r *Repository) GetTemplate(
	ctx context.Context,
	membershipID string,
	templateID string,
) (storedTemplate, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, title_ciphertext, legend_ciphertext, status,
		       published_at, created_at, updated_at
		FROM checkin_templates
		WHERE id = $1 AND professional_membership_id = $2
	`, templateID, membershipID)
	if err != nil {
		return storedTemplate{}, fmt.Errorf("query check-in template: %w", err)
	}
	defer rows.Close()
	templates, err := scanTemplates(rows)
	if err != nil {
		return storedTemplate{}, err
	}
	if len(templates) == 0 {
		return storedTemplate{}, ErrNotFound
	}
	return templates[0], nil
}

func scanTemplates(rows pgx.Rows) ([]storedTemplate, error) {
	templates := make([]storedTemplate, 0)
	for rows.Next() {
		var template storedTemplate
		if err := rows.Scan(
			&template.ID, &template.TitleCiphertext, &template.LegendCiphertext,
			&template.Status, &template.PublishedAt,
			&template.CreatedAt, &template.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan check-in template: %w", err)
		}
		templates = append(templates, template)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate check-in templates: %w", err)
	}
	return templates, nil
}

// LoadTemplateContent traz perguntas e alternativas de vários templates de
// uma vez: a listagem do profissional e a lista de check-ins do paciente
// seriam N+1 sem isso.
func (r *Repository) LoadTemplateContent(
	ctx context.Context,
	templateIDs []string,
) ([]storedQuestion, []storedOption, error) {
	if len(templateIDs) == 0 {
		return nil, nil, nil
	}
	questionRows, err := r.pool.Query(ctx, `
		SELECT id::text, template_id::text, position, prompt_ciphertext, legend_ciphertext
		FROM checkin_template_questions
		WHERE template_id = ANY($1::uuid[])
		ORDER BY template_id, position
	`, templateIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("query check-in questions: %w", err)
	}
	defer questionRows.Close()

	questions := make([]storedQuestion, 0)
	questionIDs := make([]string, 0)
	for questionRows.Next() {
		var question storedQuestion
		if err := questionRows.Scan(
			&question.ID, &question.TemplateID, &question.Position,
			&question.PromptCiphertext, &question.LegendCiphertext,
		); err != nil {
			return nil, nil, fmt.Errorf("scan check-in question: %w", err)
		}
		questions = append(questions, question)
		questionIDs = append(questionIDs, question.ID)
	}
	if err := questionRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate check-in questions: %w", err)
	}
	if len(questionIDs) == 0 {
		return questions, []storedOption{}, nil
	}

	optionRows, err := r.pool.Query(ctx, `
		SELECT id::text, question_id::text, position, score, label_ciphertext
		FROM checkin_template_options
		WHERE question_id = ANY($1::uuid[])
		ORDER BY question_id, position
	`, questionIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("query check-in options: %w", err)
	}
	defer optionRows.Close()

	options := make([]storedOption, 0)
	for optionRows.Next() {
		var option storedOption
		if err := optionRows.Scan(
			&option.ID, &option.QuestionID, &option.Position,
			&option.Score, &option.LabelCiphertext,
		); err != nil {
			return nil, nil, fmt.Errorf("scan check-in option: %w", err)
		}
		options = append(options, option)
	}
	if err := optionRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate check-in options: %w", err)
	}
	return questions, options, nil
}

/* --------------------------------------------------------- atribuições */

// CreateAssignment publica o template no mesmo ato de enviá-lo. A partir
// daqui ele é imutável, porque passa a existir resposta pendurada nele.
func (r *Repository) CreateAssignment(
	ctx context.Context,
	connectionID string,
	templateID string,
	professionalUserID string,
	membershipID string,
	now time.Time,
) (string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin create check-in assignment: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	err = tx.QueryRow(ctx, `
		SELECT status FROM checkin_templates
		WHERE id = $1 AND professional_membership_id = $2
		FOR UPDATE
	`, templateID, membershipID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("lock check-in template for assignment: %w", err)
	}
	if status == "archived" {
		return "", ErrConflict
	}

	var openCount int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM checkin_assignments
		WHERE connection_id = $1 AND status IN ('pending', 'active')
	`, connectionID).Scan(&openCount); err != nil {
		return "", fmt.Errorf("count open check-in assignments: %w", err)
	}
	if openCount >= MaxActiveAssignments {
		return "", ErrTooManyAssignments
	}

	if status == "draft" {
		if _, err := tx.Exec(ctx, `
			UPDATE checkin_templates
			SET status = 'published', published_at = $2, updated_at = $2
			WHERE id = $1
		`, templateID, now); err != nil {
			return "", fmt.Errorf("publish check-in template: %w", err)
		}
	}

	var assignmentID string
	err = tx.QueryRow(ctx, `
		INSERT INTO checkin_assignments (
			connection_id, template_id, assigned_by_professional_user_id,
			status, requested_at, updated_at
		)
		VALUES ($1, $2, $3, 'pending', $4, $4)
		RETURNING id::text
	`, connectionID, templateID, professionalUserID, now).Scan(&assignmentID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return "", ErrConflict
		}
		return "", fmt.Errorf("insert check-in assignment: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit create check-in assignment: %w", err)
	}
	return assignmentID, nil
}

const assignmentColumns = `
	assignment.id::text, assignment.connection_id::text, assignment.template_id::text,
	assignment.status, professional.display_name, patient.display_name,
	template.professional_membership_id::text,
	assignment.requested_at, assignment.responded_at, assignment.ended_at
`

func (r *Repository) ListAssignmentsForProfessional(
	ctx context.Context,
	professionalUserID string,
	connectionID string,
	limit int,
) ([]storedAssignment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+assignmentColumns+`
		FROM checkin_assignments AS assignment
		JOIN checkin_templates AS template ON template.id = assignment.template_id
		JOIN professional_patient_connections AS connection
		  ON connection.id = assignment.connection_id
		JOIN organization_memberships AS membership
		  ON membership.id = connection.professional_membership_id
		JOIN professional_users AS professional
		  ON professional.id = assignment.assigned_by_professional_user_id
		JOIN app_users AS patient ON patient.id = connection.app_user_id
		WHERE assignment.connection_id = $1
		  AND membership.professional_user_id = $2
		ORDER BY assignment.requested_at DESC
		LIMIT $3
	`, connectionID, professionalUserID, limit)
	if err != nil {
		return nil, fmt.Errorf("query professional check-in assignments: %w", err)
	}
	defer rows.Close()
	return scanAssignments(rows)
}

// ListAssignmentsForApp serve as duas telas do paciente: a inicial (todos os
// vínculos, só os ativos) e a do vínculo (com os pendentes de decisão). O
// filtro de vínculo ativo vive no SQL — um acompanhamento encerrado não pode
// continuar coletando dia a dia.
func (r *Repository) ListAssignmentsForApp(
	ctx context.Context,
	appUserID string,
	connectionID string,
	statuses []string,
	limit int,
) ([]storedAssignment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+assignmentColumns+`
		FROM checkin_assignments AS assignment
		JOIN checkin_templates AS template ON template.id = assignment.template_id
		JOIN professional_patient_connections AS connection
		  ON connection.id = assignment.connection_id
		JOIN professional_users AS professional
		  ON professional.id = assignment.assigned_by_professional_user_id
		JOIN app_users AS patient ON patient.id = connection.app_user_id
		WHERE connection.app_user_id = $1
		  AND connection.status = 'active'
		  AND ($2::uuid IS NULL OR assignment.connection_id = $2::uuid)
		  AND assignment.status = ANY($3::text[])
		ORDER BY assignment.requested_at DESC
		LIMIT $4
	`, appUserID, nullableUUID(connectionID), statuses, limit)
	if err != nil {
		return nil, fmt.Errorf("query app check-in assignments: %w", err)
	}
	defer rows.Close()
	return scanAssignments(rows)
}

// AssignmentsForApp confere posse antes de qualquer leitura em lote. É o que
// impede que uma lista de IDs vinda do cliente alcance check-in alheio.
func (r *Repository) AssignmentsForApp(
	ctx context.Context,
	appUserID string,
	assignmentIDs []string,
) ([]storedAssignment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+assignmentColumns+`
		FROM checkin_assignments AS assignment
		JOIN checkin_templates AS template ON template.id = assignment.template_id
		JOIN professional_patient_connections AS connection
		  ON connection.id = assignment.connection_id
		JOIN professional_users AS professional
		  ON professional.id = assignment.assigned_by_professional_user_id
		JOIN app_users AS patient ON patient.id = connection.app_user_id
		WHERE connection.app_user_id = $1
		  AND assignment.id = ANY($2::uuid[])
		  AND assignment.status IN ('active', 'ended', 'revoked')
		ORDER BY assignment.requested_at DESC
	`, appUserID, assignmentIDs)
	if err != nil {
		return nil, fmt.Errorf("query app check-in assignments by id: %w", err)
	}
	defer rows.Close()
	return scanAssignments(rows)
}

func scanAssignments(rows pgx.Rows) ([]storedAssignment, error) {
	assignments := make([]storedAssignment, 0)
	for rows.Next() {
		var assignment storedAssignment
		if err := rows.Scan(
			&assignment.ID, &assignment.ConnectionID, &assignment.TemplateID,
			&assignment.Status, &assignment.ProfessionalDisplayName,
			&assignment.PatientDisplayName, &assignment.AuthoredByMembershipID,
			&assignment.RequestedAt, &assignment.RespondedAt, &assignment.EndedAt,
		); err != nil {
			return nil, fmt.Errorf("scan check-in assignment: %w", err)
		}
		assignments = append(assignments, assignment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate check-in assignments: %w", err)
	}
	return assignments, nil
}

func (r *Repository) RespondToAssignment(
	ctx context.Context,
	appUserID string,
	assignmentID string,
	accepted bool,
	now time.Time,
) error {
	status := "declined"
	var endedAt *time.Time
	if accepted {
		status = "active"
	} else {
		endedAt = &now
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE checkin_assignments AS assignment
		SET status = $3, responded_at = $4, ended_at = $5, updated_at = $4
		FROM professional_patient_connections AS connection
		WHERE assignment.id = $1
		  AND connection.id = assignment.connection_id
		  AND connection.app_user_id = $2
		  AND connection.status = 'active'
		  AND assignment.status = 'pending'
	`, assignmentID, appUserID, status, now, endedAt)
	if err != nil {
		return fmt.Errorf("respond to check-in assignment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrRequestResolved
	}
	return nil
}

// EndAssignmentForApp e RevokeAssignmentForProfessional fazem a mesma
// transição por motivos diferentes, e a diferença precisa sobreviver no dado:
// quem parou o check-in é uma informação clínica.
func (r *Repository) EndAssignmentForApp(
	ctx context.Context,
	appUserID string,
	assignmentID string,
	now time.Time,
) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE checkin_assignments AS assignment
		SET status = 'ended', ended_at = $3, updated_at = $3,
		    responded_at = COALESCE(assignment.responded_at, $3)
		FROM professional_patient_connections AS connection
		WHERE assignment.id = $1
		  AND connection.id = assignment.connection_id
		  AND connection.app_user_id = $2
		  AND assignment.status IN ('pending', 'active')
	`, assignmentID, appUserID, now)
	if err != nil {
		return fmt.Errorf("end check-in assignment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) RevokeAssignmentForProfessional(
	ctx context.Context,
	professionalUserID string,
	connectionID string,
	assignmentID string,
	now time.Time,
) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE checkin_assignments AS assignment
		SET status = 'revoked', ended_at = $4, updated_at = $4
		FROM professional_patient_connections AS connection
		JOIN organization_memberships AS membership
		  ON membership.id = connection.professional_membership_id
		WHERE assignment.id = $1
		  AND connection.id = assignment.connection_id
		  AND connection.id = $2
		  AND membership.professional_user_id = $3
		  AND membership.status = 'active'
		  AND assignment.status IN ('pending', 'active')
	`, assignmentID, connectionID, professionalUserID, now)
	if err != nil {
		return fmt.Errorf("revoke check-in assignment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func nullableUUID(value string) any {
	if value == "" {
		return nil
	}
	return value
}

/* ------------------------------------------------------------ respostas */

// UpsertEntry grava o dia inteiro de uma vez. A validação de que cada
// alternativa pertence à sua pergunta é refeita aqui dentro da transação,
// mesmo com a chave estrangeira composta cobrindo o caso: o cliente não é
// fonte de verdade sobre a escala que ele mesmo respondeu.
func (r *Repository) UpsertEntry(
	ctx context.Context,
	appUserID string,
	assignmentID string,
	entryDate time.Time,
	answers []AnswerInput,
	requestHash string,
	now time.Time,
) (storedEntry, []storedAnswer, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return storedEntry{}, nil, fmt.Errorf("begin check-in entry: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var templateID string
	err = tx.QueryRow(ctx, `
		SELECT assignment.template_id::text
		FROM checkin_assignments AS assignment
		JOIN professional_patient_connections AS connection
		  ON connection.id = assignment.connection_id
		WHERE assignment.id = $1
		  AND connection.app_user_id = $2
		  AND connection.status = 'active'
		  AND assignment.status = 'active'
		FOR UPDATE OF assignment
	`, assignmentID, appUserID).Scan(&templateID)
	if errors.Is(err, pgx.ErrNoRows) {
		return storedEntry{}, nil, ErrNotFound
	}
	if err != nil {
		return storedEntry{}, nil, fmt.Errorf("lock check-in assignment: %w", err)
	}

	rows, err := tx.Query(ctx, `
		SELECT question.id::text, option.id::text, option.score
		FROM checkin_template_questions AS question
		JOIN checkin_template_options AS option ON option.question_id = question.id
		WHERE question.template_id = $1
	`, templateID)
	if err != nil {
		return storedEntry{}, nil, fmt.Errorf("query check-in scale: %w", err)
	}
	scores := make(map[string]int)
	questionIDs := make(map[string]struct{})
	for rows.Next() {
		var questionID, optionID string
		var score int
		if err := rows.Scan(&questionID, &optionID, &score); err != nil {
			rows.Close()
			return storedEntry{}, nil, fmt.Errorf("scan check-in scale: %w", err)
		}
		scores[questionID+"/"+optionID] = score
		questionIDs[questionID] = struct{}{}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return storedEntry{}, nil, fmt.Errorf("iterate check-in scale: %w", err)
	}

	if len(answers) != len(questionIDs) {
		return storedEntry{}, nil, ErrInvalidInput
	}
	answered := make(map[string]int, len(answers))
	for _, answer := range answers {
		if _, exists := answered[answer.QuestionID]; exists {
			return storedEntry{}, nil, ErrInvalidInput
		}
		score, valid := scores[answer.QuestionID+"/"+answer.OptionID]
		if !valid {
			return storedEntry{}, nil, ErrInvalidInput
		}
		answered[answer.QuestionID] = score
	}

	var entry storedEntry
	err = tx.QueryRow(ctx, `
		INSERT INTO checkin_entries (
			assignment_id, entry_date, client_request_hash, submitted_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $4)
		ON CONFLICT (assignment_id, entry_date) DO UPDATE
		SET updated_at = $4, client_request_hash = EXCLUDED.client_request_hash
		RETURNING id::text, assignment_id::text, entry_date, submitted_at, updated_at
	`, assignmentID, entryDate, nullableText(requestHash), now).Scan(
		&entry.ID, &entry.AssignmentID, &entry.EntryDate,
		&entry.SubmittedAt, &entry.UpdatedAt,
	)
	if err != nil {
		return storedEntry{}, nil, fmt.Errorf("upsert check-in entry: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM checkin_entry_answers WHERE entry_id = $1
	`, entry.ID); err != nil {
		return storedEntry{}, nil, fmt.Errorf("clear check-in answers: %w", err)
	}

	stored := make([]storedAnswer, 0, len(answers))
	for _, answer := range answers {
		score := answered[answer.QuestionID]
		if _, err := tx.Exec(ctx, `
			INSERT INTO checkin_entry_answers (
				entry_id, question_id, option_id, score, created_at
			)
			VALUES ($1, $2, $3, $4, $5)
		`, entry.ID, answer.QuestionID, answer.OptionID, score, now); err != nil {
			return storedEntry{}, nil, fmt.Errorf("insert check-in answer: %w", err)
		}
		stored = append(stored, storedAnswer{
			EntryID: entry.ID, QuestionID: answer.QuestionID,
			OptionID: answer.OptionID, Score: score,
		})
	}

	if err := tx.Commit(ctx); err != nil {
		return storedEntry{}, nil, fmt.Errorf("commit check-in entry: %w", err)
	}
	return entry, stored, nil
}

// LoadEntries carrega dias e respostas de várias atribuições ao mesmo tempo.
// A posse já precisa ter sido conferida por quem chama.
func (r *Repository) LoadEntries(
	ctx context.Context,
	assignmentIDs []string,
	periodStart time.Time,
	periodEnd time.Time,
) ([]storedEntry, []storedAnswer, error) {
	if len(assignmentIDs) == 0 {
		return []storedEntry{}, []storedAnswer{}, nil
	}
	entryRows, err := r.pool.Query(ctx, `
		SELECT id::text, assignment_id::text, entry_date, submitted_at, updated_at
		FROM checkin_entries
		WHERE assignment_id = ANY($1::uuid[])
		  AND entry_date BETWEEN $2 AND $3
		ORDER BY assignment_id, entry_date
	`, assignmentIDs, periodStart, periodEnd)
	if err != nil {
		return nil, nil, fmt.Errorf("query check-in entries: %w", err)
	}
	defer entryRows.Close()

	entries := make([]storedEntry, 0)
	entryIDs := make([]string, 0)
	for entryRows.Next() {
		var entry storedEntry
		if err := entryRows.Scan(
			&entry.ID, &entry.AssignmentID, &entry.EntryDate,
			&entry.SubmittedAt, &entry.UpdatedAt,
		); err != nil {
			return nil, nil, fmt.Errorf("scan check-in entry: %w", err)
		}
		entries = append(entries, entry)
		entryIDs = append(entryIDs, entry.ID)
	}
	if err := entryRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate check-in entries: %w", err)
	}
	if len(entryIDs) == 0 {
		return entries, []storedAnswer{}, nil
	}

	answerRows, err := r.pool.Query(ctx, `
		SELECT entry_id::text, question_id::text, option_id::text, score
		FROM checkin_entry_answers
		WHERE entry_id = ANY($1::uuid[])
	`, entryIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("query check-in answers: %w", err)
	}
	defer answerRows.Close()

	answers := make([]storedAnswer, 0)
	for answerRows.Next() {
		var answer storedAnswer
		if err := answerRows.Scan(
			&answer.EntryID, &answer.QuestionID, &answer.OptionID, &answer.Score,
		); err != nil {
			return nil, nil, fmt.Errorf("scan check-in answer: %w", err)
		}
		answers = append(answers, answer)
	}
	if err := answerRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate check-in answers: %w", err)
	}
	return entries, answers, nil
}

type entryStats struct {
	AnsweredDays  int
	LastEntryDate *time.Time
}

func (r *Repository) EntryStats(
	ctx context.Context,
	assignmentIDs []string,
) (map[string]entryStats, error) {
	stats := make(map[string]entryStats)
	if len(assignmentIDs) == 0 {
		return stats, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT assignment_id::text, count(*), max(entry_date)
		FROM checkin_entries
		WHERE assignment_id = ANY($1::uuid[])
		GROUP BY assignment_id
	`, assignmentIDs)
	if err != nil {
		return nil, fmt.Errorf("query check-in entry stats: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var assignmentID string
		var stat entryStats
		if err := rows.Scan(&assignmentID, &stat.AnsweredDays, &stat.LastEntryDate); err != nil {
			return nil, fmt.Errorf("scan check-in entry stats: %w", err)
		}
		stats[assignmentID] = stat
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate check-in entry stats: %w", err)
	}
	return stats, nil
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

/* ------------------------------------------------------------ colheita */

func (r *Repository) CreateCollectionRequest(
	ctx context.Context,
	connectionID string,
	professionalUserID string,
	periodStart time.Time,
	periodEnd time.Time,
	now time.Time,
) (storedCollectionRequest, error) {
	var request storedCollectionRequest
	err := r.pool.QueryRow(ctx, `
		INSERT INTO checkin_collection_requests (
			connection_id, requested_by_professional_user_id,
			period_start, period_end, status, requested_at, updated_at
		)
		VALUES ($1, $2, $3, $4, 'pending', $5, $5)
		RETURNING id::text, connection_id::text, period_start, period_end,
		          status, requested_at, responded_at
	`, connectionID, professionalUserID, periodStart, periodEnd, now).Scan(
		&request.ID, &request.ConnectionID, &request.PeriodStart, &request.PeriodEnd,
		&request.Status, &request.RequestedAt, &request.RespondedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return storedCollectionRequest{}, ErrConflict
		}
		return storedCollectionRequest{}, fmt.Errorf("create check-in collection request: %w", err)
	}
	return request, nil
}

const collectionRequestColumns = `
	request.id::text, request.connection_id::text,
	professional.display_name, patient.display_name,
	request.period_start, request.period_end, request.status,
	request.requested_at, request.responded_at
`

func (r *Repository) ListCollectionRequestsForProfessional(
	ctx context.Context,
	professionalUserID string,
	connectionID string,
	limit int,
) ([]storedCollectionRequest, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+collectionRequestColumns+`
		FROM checkin_collection_requests AS request
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
		return nil, fmt.Errorf("query professional collection requests: %w", err)
	}
	defer rows.Close()
	return scanCollectionRequests(rows)
}

func (r *Repository) ListCollectionRequestsForApp(
	ctx context.Context,
	appUserID string,
	connectionID string,
	limit int,
) ([]storedCollectionRequest, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+collectionRequestColumns+`
		FROM checkin_collection_requests AS request
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
		return nil, fmt.Errorf("query app collection requests: %w", err)
	}
	defer rows.Close()
	return scanCollectionRequests(rows)
}

func scanCollectionRequests(rows pgx.Rows) ([]storedCollectionRequest, error) {
	requests := make([]storedCollectionRequest, 0)
	for rows.Next() {
		var request storedCollectionRequest
		if err := rows.Scan(
			&request.ID, &request.ConnectionID,
			&request.ProfessionalDisplayName, &request.PatientDisplayName,
			&request.PeriodStart, &request.PeriodEnd, &request.Status,
			&request.RequestedAt, &request.RespondedAt,
		); err != nil {
			return nil, fmt.Errorf("scan collection request: %w", err)
		}
		requests = append(requests, request)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate collection requests: %w", err)
	}
	return requests, nil
}

func (r *Repository) CollectionRequestForApp(
	ctx context.Context,
	appUserID string,
	requestID string,
) (storedCollectionRequest, error) {
	var request storedCollectionRequest
	err := r.pool.QueryRow(ctx, `
		SELECT `+collectionRequestColumns+`
		FROM checkin_collection_requests AS request
		JOIN professional_patient_connections AS connection
		  ON connection.id = request.connection_id
		JOIN professional_users AS professional
		  ON professional.id = request.requested_by_professional_user_id
		JOIN app_users AS patient ON patient.id = connection.app_user_id
		WHERE request.id = $1
		  AND connection.app_user_id = $2
		  AND connection.status = 'active'
	`, requestID, appUserID).Scan(
		&request.ID, &request.ConnectionID,
		&request.ProfessionalDisplayName, &request.PatientDisplayName,
		&request.PeriodStart, &request.PeriodEnd, &request.Status,
		&request.RequestedAt, &request.RespondedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return storedCollectionRequest{}, ErrNotFound
	}
	if err != nil {
		return storedCollectionRequest{}, fmt.Errorf("load collection request: %w", err)
	}
	return request, nil
}

func (r *Repository) DeclineCollectionRequest(
	ctx context.Context,
	appUserID string,
	requestID string,
	now time.Time,
) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE checkin_collection_requests AS request
		SET status = 'declined', responded_at = $3, updated_at = $3
		FROM professional_patient_connections AS connection
		WHERE request.id = $1
		  AND connection.id = request.connection_id
		  AND connection.app_user_id = $2
		  AND request.status = 'pending'
	`, requestID, appUserID, now)
	if err != nil {
		return fmt.Errorf("decline collection request: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrRequestResolved
	}
	return nil
}

// CompleteCollectionRequest fecha o pedido e grava o retrato no mesmo passo.
// O `FOR UPDATE` com nova checagem de status é o que impede que dois cliques
// simultâneos entreguem dois retratos do mesmo pedido.
func (r *Repository) CompleteCollectionRequest(
	ctx context.Context,
	appUserID string,
	requestID string,
	checkinCount int,
	payloadCiphertext []byte,
	now time.Time,
) (storedCollection, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return storedCollection{}, fmt.Errorf("begin complete collection: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var connectionID, status string
	var periodStart, periodEnd time.Time
	err = tx.QueryRow(ctx, `
		SELECT request.connection_id::text, request.status,
		       request.period_start, request.period_end
		FROM checkin_collection_requests AS request
		JOIN professional_patient_connections AS connection
		  ON connection.id = request.connection_id
		WHERE request.id = $1
		  AND connection.app_user_id = $2
		  AND connection.status = 'active'
		FOR UPDATE OF request
	`, requestID, appUserID).Scan(&connectionID, &status, &periodStart, &periodEnd)
	if errors.Is(err, pgx.ErrNoRows) {
		return storedCollection{}, ErrNotFound
	}
	if err != nil {
		return storedCollection{}, fmt.Errorf("lock collection request: %w", err)
	}
	if status != "pending" {
		return storedCollection{}, ErrRequestResolved
	}

	var collection storedCollection
	err = tx.QueryRow(ctx, `
		INSERT INTO checkin_collections (
			request_id, connection_id, period_start, period_end,
			checkin_count, payload_ciphertext, shared_at, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
		RETURNING id::text, connection_id::text, request_id::text,
		          period_start, period_end, payload_ciphertext, shared_at
	`, requestID, connectionID, periodStart, periodEnd,
		checkinCount, payloadCiphertext, now).Scan(
		&collection.ID, &collection.ConnectionID, &collection.RequestID,
		&collection.PeriodStart, &collection.PeriodEnd,
		&collection.PayloadCiphertext, &collection.SharedAt,
	)
	if err != nil {
		return storedCollection{}, fmt.Errorf("insert check-in collection: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE checkin_collection_requests
		SET status = 'sent', responded_at = $2, updated_at = $2
		WHERE id = $1
	`, requestID, now); err != nil {
		return storedCollection{}, fmt.Errorf("resolve collection request: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return storedCollection{}, fmt.Errorf("commit complete collection: %w", err)
	}
	return collection, nil
}

func (r *Repository) ListCollections(
	ctx context.Context,
	connectionID string,
	limit int,
) ([]storedCollection, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, connection_id::text, request_id::text,
		       period_start, period_end, payload_ciphertext, shared_at
		FROM checkin_collections
		WHERE connection_id = $1
		ORDER BY shared_at DESC
		LIMIT $2
	`, connectionID, limit)
	if err != nil {
		return nil, fmt.Errorf("query check-in collections: %w", err)
	}
	defer rows.Close()

	collections := make([]storedCollection, 0)
	for rows.Next() {
		var collection storedCollection
		if err := rows.Scan(
			&collection.ID, &collection.ConnectionID, &collection.RequestID,
			&collection.PeriodStart, &collection.PeriodEnd,
			&collection.PayloadCiphertext, &collection.SharedAt,
		); err != nil {
			return nil, fmt.Errorf("scan check-in collection: %w", err)
		}
		collections = append(collections, collection)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate check-in collections: %w", err)
	}
	return collections, nil
}

// TemplateHeaders busca os cabeçalhos sem passar pela filiação: quem chama já
// provou acesso pela atribuição, e o paciente precisa ler o título de um
// template que não é dele.
func (r *Repository) TemplateHeaders(
	ctx context.Context,
	templateIDs []string,
) (map[string]storedTemplate, error) {
	headers := make(map[string]storedTemplate)
	if len(templateIDs) == 0 {
		return headers, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, title_ciphertext, legend_ciphertext, status,
		       published_at, created_at, updated_at
		FROM checkin_templates
		WHERE id = ANY($1::uuid[])
	`, templateIDs)
	if err != nil {
		return nil, fmt.Errorf("query check-in template headers: %w", err)
	}
	defer rows.Close()
	templates, err := scanTemplates(rows)
	if err != nil {
		return nil, err
	}
	for _, template := range templates {
		headers[template.ID] = template
	}
	return headers, nil
}
