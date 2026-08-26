package checkin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	maxTitleRunes         = 120
	maxLegendRunes        = 500
	maxPromptRunes        = 200
	maxQuestionLegendRune = 300
	maxOptionLabelRunes   = 60
	listLimit             = 50
)

type Cipher interface {
	Encrypt([]byte) ([]byte, error)
	Decrypt([]byte) ([]byte, error)
}

type Service struct {
	repository *Repository
	cipher     Cipher
	now        func() time.Time
}

func NewService(repository *Repository, cipher Cipher) (*Service, error) {
	if repository == nil || cipher == nil {
		return nil, fmt.Errorf("check-in service dependencies are required")
	}
	return &Service{repository: repository, cipher: cipher, now: time.Now}, nil
}

/* ----------------------------------------------------------- templates */

func (s *Service) CreateTemplate(
	ctx context.Context,
	professionalUserID string,
	input TemplateInput,
) (Template, error) {
	professional, err := s.authorizeProfessional(ctx, professionalUserID)
	if err != nil {
		return Template{}, err
	}
	write, err := s.templateWriteFromInput(input)
	if err != nil {
		return Template{}, err
	}
	templateID, err := s.repository.CreateTemplate(
		ctx, professional.MembershipID, professional.OrganizationID, write, s.now().UTC(),
	)
	if err != nil {
		return Template{}, err
	}
	return s.GetTemplate(ctx, professionalUserID, templateID)
}

func (s *Service) UpdateTemplate(
	ctx context.Context,
	professionalUserID string,
	templateID string,
	input TemplateInput,
) (Template, error) {
	if _, err := uuid.Parse(templateID); err != nil {
		return Template{}, ErrInvalidInput
	}
	professional, err := s.authorizeProfessional(ctx, professionalUserID)
	if err != nil {
		return Template{}, err
	}
	write, err := s.templateWriteFromInput(input)
	if err != nil {
		return Template{}, err
	}
	if err := s.repository.ReplaceTemplateContent(
		ctx, professional.MembershipID, templateID, write, s.now().UTC(),
	); err != nil {
		return Template{}, err
	}
	return s.GetTemplate(ctx, professionalUserID, templateID)
}

func (s *Service) ListTemplates(
	ctx context.Context,
	professionalUserID string,
) ([]Template, error) {
	professional, err := s.authorizeProfessional(ctx, professionalUserID)
	if err != nil {
		return nil, err
	}
	stored, err := s.repository.ListTemplates(ctx, professional.MembershipID, listLimit)
	if err != nil {
		return nil, err
	}
	return s.templatesFromStored(ctx, stored)
}

func (s *Service) GetTemplate(
	ctx context.Context,
	professionalUserID string,
	templateID string,
) (Template, error) {
	if _, err := uuid.Parse(templateID); err != nil {
		return Template{}, ErrInvalidInput
	}
	professional, err := s.authorizeProfessional(ctx, professionalUserID)
	if err != nil {
		return Template{}, err
	}
	stored, err := s.repository.GetTemplate(ctx, professional.MembershipID, templateID)
	if err != nil {
		return Template{}, err
	}
	templates, err := s.templatesFromStored(ctx, []storedTemplate{stored})
	if err != nil {
		return Template{}, err
	}
	return templates[0], nil
}

func (s *Service) ArchiveTemplate(
	ctx context.Context,
	professionalUserID string,
	templateID string,
) error {
	if _, err := uuid.Parse(templateID); err != nil {
		return ErrInvalidInput
	}
	professional, err := s.authorizeProfessional(ctx, professionalUserID)
	if err != nil {
		return err
	}
	return s.repository.ArchiveTemplate(
		ctx, professional.MembershipID, templateID, s.now().UTC(),
	)
}

// authorizeProfessional concentra o que toda rota profissional exige. A UI
// esconde botões; isto é o que de fato recusa.
func (s *Service) authorizeProfessional(
	ctx context.Context,
	professionalUserID string,
) (professionalContext, error) {
	professional, err := s.repository.ProfessionalContext(ctx, professionalUserID)
	if err != nil {
		return professionalContext{}, err
	}
	if !professional.ProfileComplete {
		return professionalContext{}, ErrProfileIncomplete
	}
	if professional.SubscriptionStatus != "active" &&
		professional.SubscriptionStatus != "trialing" {
		return professionalContext{}, ErrSubscriptionRequired
	}
	return professional, nil
}

func (s *Service) templateWriteFromInput(input TemplateInput) (templateWrite, error) {
	title := strings.TrimSpace(input.Title)
	legend := strings.TrimSpace(input.Legend)
	if utf8.RuneCountInString(title) < 1 || utf8.RuneCountInString(title) > maxTitleRunes ||
		utf8.RuneCountInString(legend) > maxLegendRunes ||
		len(input.Questions) < MinQuestionsPerTemplate ||
		len(input.Questions) > MaxQuestionsPerTemplate {
		return templateWrite{}, ErrInvalidInput
	}

	titleCiphertext, err := s.cipher.Encrypt([]byte(title))
	if err != nil {
		return templateWrite{}, fmt.Errorf("encrypt check-in title: %w", err)
	}
	legendCiphertext, err := s.encryptOptional(legend)
	if err != nil {
		return templateWrite{}, fmt.Errorf("encrypt check-in legend: %w", err)
	}

	questions := make([]questionWrite, 0, len(input.Questions))
	for _, question := range input.Questions {
		prompt := strings.TrimSpace(question.Prompt)
		questionLegend := strings.TrimSpace(question.Legend)
		if utf8.RuneCountInString(prompt) < 1 || utf8.RuneCountInString(prompt) > maxPromptRunes ||
			utf8.RuneCountInString(questionLegend) > maxQuestionLegendRune ||
			len(question.Options) != OptionsPerQuestion {
			return templateWrite{}, ErrInvalidInput
		}
		promptCiphertext, err := s.cipher.Encrypt([]byte(prompt))
		if err != nil {
			return templateWrite{}, fmt.Errorf("encrypt check-in prompt: %w", err)
		}
		questionLegendCiphertext, err := s.encryptOptional(questionLegend)
		if err != nil {
			return templateWrite{}, fmt.Errorf("encrypt check-in question legend: %w", err)
		}

		options := make([]optionWrite, 0, len(question.Options))
		for index, option := range question.Options {
			label := strings.TrimSpace(option.Label)
			if utf8.RuneCountInString(label) < 1 ||
				utf8.RuneCountInString(label) > maxOptionLabelRunes {
				return templateWrite{}, ErrInvalidInput
			}
			labelCiphertext, err := s.cipher.Encrypt([]byte(label))
			if err != nil {
				return templateWrite{}, fmt.Errorf("encrypt check-in option: %w", err)
			}
			// A nota é a posição: o primeiro rótulo é o extremo mais baixo.
			options = append(options, optionWrite{
				LabelCiphertext: labelCiphertext, Score: index + MinOptionScore,
			})
		}
		questions = append(questions, questionWrite{
			PromptCiphertext: promptCiphertext,
			LegendCiphertext: questionLegendCiphertext,
			Options:          options,
		})
	}

	return templateWrite{
		TitleCiphertext:  titleCiphertext,
		LegendCiphertext: legendCiphertext,
		Questions:        questions,
	}, nil
}

func (s *Service) templatesFromStored(
	ctx context.Context,
	stored []storedTemplate,
) ([]Template, error) {
	templates := make([]Template, 0, len(stored))
	if len(stored) == 0 {
		return templates, nil
	}
	templateIDs := make([]string, 0, len(stored))
	for _, item := range stored {
		templateIDs = append(templateIDs, item.ID)
	}
	questions, options, err := s.repository.LoadTemplateContent(ctx, templateIDs)
	if err != nil {
		return nil, err
	}
	byTemplate, err := s.questionsByTemplate(questions, options)
	if err != nil {
		return nil, err
	}
	for _, item := range stored {
		title, err := s.cipher.Decrypt(item.TitleCiphertext)
		if err != nil {
			return nil, fmt.Errorf("decrypt check-in title: %w", err)
		}
		legend, err := s.decryptOptional(item.LegendCiphertext)
		if err != nil {
			return nil, fmt.Errorf("decrypt check-in legend: %w", err)
		}
		templates = append(templates, Template{
			ID: item.ID, Title: string(title), Legend: legend, Status: item.Status,
			Questions:   byTemplate[item.ID],
			PublishedAt: item.PublishedAt,
			CreatedAt:   item.CreatedAt, UpdatedAt: item.UpdatedAt,
		})
	}
	return templates, nil
}

func (s *Service) questionsByTemplate(
	questions []storedQuestion,
	options []storedOption,
) (map[string][]Question, error) {
	optionsByQuestion := make(map[string][]Option, len(questions))
	for _, option := range options {
		label, err := s.cipher.Decrypt(option.LabelCiphertext)
		if err != nil {
			return nil, fmt.Errorf("decrypt check-in option: %w", err)
		}
		optionsByQuestion[option.QuestionID] = append(
			optionsByQuestion[option.QuestionID],
			Option{
				ID: option.ID, Position: option.Position,
				Label: string(label), Score: option.Score,
			},
		)
	}

	byTemplate := make(map[string][]Question)
	for _, question := range questions {
		prompt, err := s.cipher.Decrypt(question.PromptCiphertext)
		if err != nil {
			return nil, fmt.Errorf("decrypt check-in prompt: %w", err)
		}
		legend, err := s.decryptOptional(question.LegendCiphertext)
		if err != nil {
			return nil, fmt.Errorf("decrypt check-in question legend: %w", err)
		}
		byTemplate[question.TemplateID] = append(byTemplate[question.TemplateID], Question{
			ID: question.ID, Position: question.Position, Prompt: string(prompt),
			Legend: legend, Options: optionsByQuestion[question.ID],
		})
	}
	return byTemplate, nil
}

func (s *Service) encryptOptional(value string) ([]byte, error) {
	if value == "" {
		return nil, nil
	}
	return s.cipher.Encrypt([]byte(value))
}

func (s *Service) decryptOptional(ciphertext []byte) (string, error) {
	if len(ciphertext) == 0 {
		return "", nil
	}
	plaintext, err := s.cipher.Decrypt(ciphertext)
	return string(plaintext), err
}

func parseDate(value string) (time.Time, error) {
	parsed, err := time.Parse(DateLayout, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, ErrInvalidInput
	}
	return parsed.UTC(), nil
}

func formatDate(value time.Time) string {
	return value.UTC().Format(DateLayout)
}

func round2(value float64) float64 {
	return math.Round(value*100) / 100
}

/* --------------------------------------------------------- atribuições */

func (s *Service) AssignTemplate(
	ctx context.Context,
	professionalUserID string,
	connectionID string,
	templateID string,
) (Assignment, error) {
	if _, err := uuid.Parse(connectionID); err != nil {
		return Assignment{}, ErrInvalidInput
	}
	if _, err := uuid.Parse(templateID); err != nil {
		return Assignment{}, ErrInvalidInput
	}
	access, err := s.authorizeProfessionalConnection(ctx, professionalUserID, connectionID)
	if err != nil {
		return Assignment{}, err
	}
	assignmentID, err := s.repository.CreateAssignment(
		ctx, connectionID, templateID, professionalUserID,
		access.MembershipID, s.now().UTC(),
	)
	if err != nil {
		return Assignment{}, err
	}
	assignments, err := s.ListAssignmentsForProfessional(
		ctx, professionalUserID, connectionID,
	)
	if err != nil {
		return Assignment{}, err
	}
	for _, assignment := range assignments {
		if assignment.ID == assignmentID {
			return assignment, nil
		}
	}
	return Assignment{}, ErrNotFound
}

func (s *Service) ListAssignmentsForProfessional(
	ctx context.Context,
	professionalUserID string,
	connectionID string,
) ([]Assignment, error) {
	if _, err := uuid.Parse(connectionID); err != nil {
		return nil, ErrInvalidInput
	}
	if _, err := s.authorizeProfessionalConnection(
		ctx, professionalUserID, connectionID,
	); err != nil {
		return nil, err
	}
	stored, err := s.repository.ListAssignmentsForProfessional(
		ctx, professionalUserID, connectionID, listLimit,
	)
	if err != nil {
		return nil, err
	}
	// O profissional vê o estado do check-in que ele mandou — nunca as
	// respostas dia a dia. Isso só chega pela colheita autorizada.
	return s.assignmentsFromStored(ctx, stored, time.Time{}, false)
}

func (s *Service) RevokeAssignment(
	ctx context.Context,
	professionalUserID string,
	connectionID string,
	assignmentID string,
) error {
	if _, err := uuid.Parse(connectionID); err != nil {
		return ErrInvalidInput
	}
	if _, err := uuid.Parse(assignmentID); err != nil {
		return ErrInvalidInput
	}
	if _, err := s.authorizeProfessionalConnection(
		ctx, professionalUserID, connectionID,
	); err != nil {
		return err
	}
	return s.repository.RevokeAssignmentForProfessional(
		ctx, professionalUserID, connectionID, assignmentID, s.now().UTC(),
	)
}

// ListAssignmentsForApp responde as duas perguntas do paciente: "o que eu
// respondo hoje" (sem vínculo, status ativo) e "o que estão me pedindo"
// (por vínculo, status pendente).
func (s *Service) ListAssignmentsForApp(
	ctx context.Context,
	appUserID string,
	connectionID string,
	statuses []string,
	localDate string,
) ([]Assignment, error) {
	if connectionID != "" {
		if _, err := uuid.Parse(connectionID); err != nil {
			return nil, ErrInvalidInput
		}
		if err := s.repository.AppOwnsConnection(ctx, appUserID, connectionID); err != nil {
			return nil, err
		}
	}
	for _, status := range statuses {
		if !slices.Contains(
			[]string{"pending", "active", "declined", "revoked", "ended"}, status,
		) {
			return nil, ErrInvalidInput
		}
	}
	if len(statuses) == 0 {
		statuses = []string{"active"}
	}
	today, err := s.resolveLocalDate(localDate)
	if err != nil {
		return nil, err
	}
	stored, err := s.repository.ListAssignmentsForApp(
		ctx, appUserID, connectionID, statuses, listLimit,
	)
	if err != nil {
		return nil, err
	}
	return s.assignmentsFromStored(ctx, stored, today, true)
}

func (s *Service) RespondToAssignment(
	ctx context.Context,
	appUserID string,
	assignmentID string,
	accepted bool,
) error {
	if _, err := uuid.Parse(assignmentID); err != nil {
		return ErrInvalidInput
	}
	return s.repository.RespondToAssignment(
		ctx, appUserID, assignmentID, accepted, s.now().UTC(),
	)
}

func (s *Service) EndAssignment(
	ctx context.Context,
	appUserID string,
	assignmentID string,
) error {
	if _, err := uuid.Parse(assignmentID); err != nil {
		return ErrInvalidInput
	}
	return s.repository.EndAssignmentForApp(
		ctx, appUserID, assignmentID, s.now().UTC(),
	)
}

func (s *Service) authorizeProfessionalConnection(
	ctx context.Context,
	professionalUserID string,
	connectionID string,
) (connectionAccess, error) {
	access, err := s.repository.ProfessionalConnectionAccess(
		ctx, professionalUserID, connectionID,
	)
	if err != nil {
		return connectionAccess{}, err
	}
	if !access.ProfileComplete {
		return connectionAccess{}, ErrProfileIncomplete
	}
	if access.SubscriptionStatus != "active" && access.SubscriptionStatus != "trialing" {
		return connectionAccess{}, ErrSubscriptionRequired
	}
	return access, nil
}

// resolveLocalDate aceita o dia local do aparelho, mas só dentro de um dia de
// distância do agora em UTC. Isso cobre qualquer fuso do mundo sem deixar o
// cliente escolher livremente a data de um registro diário.
func (s *Service) resolveLocalDate(value string) (time.Time, error) {
	now := s.now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if strings.TrimSpace(value) == "" {
		return today, nil
	}
	parsed, err := parseDate(value)
	if err != nil {
		return time.Time{}, err
	}
	difference := parsed.Sub(today)
	if difference > 24*time.Hour || difference < -24*time.Hour {
		return time.Time{}, ErrInvalidInput
	}
	return parsed, nil
}

func (s *Service) assignmentsFromStored(
	ctx context.Context,
	stored []storedAssignment,
	today time.Time,
	includeEntries bool,
) ([]Assignment, error) {
	assignments := make([]Assignment, 0, len(stored))
	if len(stored) == 0 {
		return assignments, nil
	}

	templateIDs := make([]string, 0, len(stored))
	assignmentIDs := make([]string, 0, len(stored))
	for _, item := range stored {
		templateIDs = append(templateIDs, item.TemplateID)
		assignmentIDs = append(assignmentIDs, item.ID)
	}
	questions, options, err := s.repository.LoadTemplateContent(ctx, templateIDs)
	if err != nil {
		return nil, err
	}
	byTemplate, err := s.questionsByTemplate(questions, options)
	if err != nil {
		return nil, err
	}
	headers, err := s.repository.TemplateHeaders(ctx, templateIDs)
	if err != nil {
		return nil, err
	}

	stats := map[string]entryStats{}
	todayEntries := map[string]*Entry{}
	if includeEntries {
		stats, err = s.repository.EntryStats(ctx, assignmentIDs)
		if err != nil {
			return nil, err
		}
		entries, answers, err := s.repository.LoadEntries(
			ctx, assignmentIDs, today, today,
		)
		if err != nil {
			return nil, err
		}
		answersByEntry := make(map[string][]Answer, len(entries))
		for _, answer := range answers {
			answersByEntry[answer.EntryID] = append(answersByEntry[answer.EntryID], Answer{
				QuestionID: answer.QuestionID, OptionID: answer.OptionID,
				Score: answer.Score,
			})
		}
		for _, entry := range entries {
			todayEntries[entry.AssignmentID] = &Entry{
				ID: entry.ID, AssignmentID: entry.AssignmentID,
				EntryDate:   formatDate(entry.EntryDate),
				Answers:     answersByEntry[entry.ID],
				SubmittedAt: entry.SubmittedAt, UpdatedAt: entry.UpdatedAt,
			}
		}
	}

	for _, item := range stored {
		header, exists := headers[item.TemplateID]
		if !exists {
			return nil, ErrNotFound
		}
		template, err := s.templateFromHeader(header, byTemplate[item.TemplateID])
		if err != nil {
			return nil, err
		}
		assignment := Assignment{
			ID: item.ID, ConnectionID: item.ConnectionID, Status: item.Status,
			ProfessionalDisplayName: item.ProfessionalDisplayName,
			PatientDisplayName:      item.PatientDisplayName,
			Template:                template,
			RequestedAt:             item.RequestedAt,
			RespondedAt:             item.RespondedAt,
			EndedAt:                 item.EndedAt,
		}
		if stat, exists := stats[item.ID]; exists {
			assignment.AnsweredDays = stat.AnsweredDays
			if stat.LastEntryDate != nil {
				assignment.LastEntryDate = formatDate(*stat.LastEntryDate)
			}
		}
		if entry, exists := todayEntries[item.ID]; exists {
			assignment.AnsweredToday = true
			assignment.TodayEntry = entry
		}
		assignments = append(assignments, assignment)
	}
	return assignments, nil
}

// templateFromHeader decifra o cabeçalho já carregado em lote e o junta às
// perguntas. O paciente precisa ler o título de um template que não é dele,
// então esta leitura não passa pela filiação — a posse já foi provada pela
// atribuição que trouxe o template até aqui.
func (s *Service) templateFromHeader(
	header storedTemplate,
	questions []Question,
) (Template, error) {
	title, err := s.cipher.Decrypt(header.TitleCiphertext)
	if err != nil {
		return Template{}, fmt.Errorf("decrypt check-in title: %w", err)
	}
	legend, err := s.decryptOptional(header.LegendCiphertext)
	if err != nil {
		return Template{}, fmt.Errorf("decrypt check-in legend: %w", err)
	}
	return Template{
		ID: header.ID, Title: string(title), Legend: legend, Status: header.Status,
		Questions: questions, PublishedAt: header.PublishedAt,
		CreatedAt: header.CreatedAt, UpdatedAt: header.UpdatedAt,
	}, nil
}

/* ------------------------------------------------------------ respostas */

func (s *Service) SubmitEntry(
	ctx context.Context,
	appUserID string,
	assignmentID string,
	entryDate string,
	answers []AnswerInput,
	idempotencyKey string,
) (Entry, error) {
	if _, err := uuid.Parse(assignmentID); err != nil {
		return Entry{}, ErrInvalidInput
	}
	if len(answers) < MinQuestionsPerTemplate || len(answers) > MaxQuestionsPerTemplate {
		return Entry{}, ErrInvalidInput
	}
	for _, answer := range answers {
		if _, err := uuid.Parse(answer.QuestionID); err != nil {
			return Entry{}, ErrInvalidInput
		}
		if _, err := uuid.Parse(answer.OptionID); err != nil {
			return Entry{}, ErrInvalidInput
		}
	}
	day, err := s.resolveLocalDate(entryDate)
	if err != nil {
		return Entry{}, err
	}
	requestHash := ""
	if strings.TrimSpace(idempotencyKey) != "" {
		digest := sha256.Sum256([]byte(idempotencyKey))
		requestHash = hex.EncodeToString(digest[:])
	}
	entry, stored, err := s.repository.UpsertEntry(
		ctx, appUserID, assignmentID, day, answers, requestHash, s.now().UTC(),
	)
	if err != nil {
		return Entry{}, err
	}
	result := Entry{
		ID: entry.ID, AssignmentID: entry.AssignmentID,
		EntryDate: formatDate(entry.EntryDate), Answers: make([]Answer, 0, len(stored)),
		SubmittedAt: entry.SubmittedAt, UpdatedAt: entry.UpdatedAt,
	}
	for _, answer := range stored {
		result.Answers = append(result.Answers, Answer{
			QuestionID: answer.QuestionID, OptionID: answer.OptionID, Score: answer.Score,
		})
	}
	return result, nil
}

func (s *Service) ListEntries(
	ctx context.Context,
	appUserID string,
	assignmentID string,
	from string,
	to string,
) ([]Entry, error) {
	if _, err := uuid.Parse(assignmentID); err != nil {
		return nil, ErrInvalidInput
	}
	owned, err := s.repository.AssignmentsForApp(ctx, appUserID, []string{assignmentID})
	if err != nil {
		return nil, err
	}
	if len(owned) == 0 {
		return nil, ErrNotFound
	}
	periodEnd, err := parseDate(to)
	if err != nil {
		return nil, err
	}
	periodStart, err := parseDate(from)
	if err != nil {
		return nil, err
	}
	if periodEnd.Before(periodStart) ||
		periodEnd.Sub(periodStart) > MaxCollectionDays*24*time.Hour {
		return nil, ErrInvalidInput
	}
	entries, answers, err := s.repository.LoadEntries(
		ctx, []string{assignmentID}, periodStart, periodEnd,
	)
	if err != nil {
		return nil, err
	}
	answersByEntry := make(map[string][]Answer, len(entries))
	for _, answer := range answers {
		answersByEntry[answer.EntryID] = append(answersByEntry[answer.EntryID], Answer{
			QuestionID: answer.QuestionID, OptionID: answer.OptionID, Score: answer.Score,
		})
	}
	result := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, Entry{
			ID: entry.ID, AssignmentID: entry.AssignmentID,
			EntryDate: formatDate(entry.EntryDate), Answers: answersByEntry[entry.ID],
			SubmittedAt: entry.SubmittedAt, UpdatedAt: entry.UpdatedAt,
		})
	}
	return result, nil
}

/* ------------------------------------------------------------ colheita */

func (s *Service) CreateCollectionRequest(
	ctx context.Context,
	professionalUserID string,
	connectionID string,
	periodStart string,
	periodEnd string,
) (CollectionRequest, error) {
	if _, err := uuid.Parse(connectionID); err != nil {
		return CollectionRequest{}, ErrInvalidInput
	}
	access, err := s.authorizeProfessionalConnection(ctx, professionalUserID, connectionID)
	if err != nil {
		return CollectionRequest{}, err
	}
	start, err := parseDate(periodStart)
	if err != nil {
		return CollectionRequest{}, err
	}
	end, err := parseDate(periodEnd)
	if err != nil {
		return CollectionRequest{}, err
	}
	now := s.now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if end.Before(start) || end.Sub(start) > MaxCollectionDays*24*time.Hour ||
		end.After(today) || start.Before(access.ActivatedAt.UTC().Truncate(24*time.Hour)) {
		return CollectionRequest{}, ErrInvalidInput
	}

	stored, err := s.repository.CreateCollectionRequest(
		ctx, connectionID, professionalUserID, start, end, now,
	)
	if err != nil {
		return CollectionRequest{}, err
	}
	return collectionRequestFromStored(stored), nil
}

func (s *Service) ListCollectionRequestsForProfessional(
	ctx context.Context,
	professionalUserID string,
	connectionID string,
) ([]CollectionRequest, error) {
	if _, err := uuid.Parse(connectionID); err != nil {
		return nil, ErrInvalidInput
	}
	if _, err := s.authorizeProfessionalConnection(
		ctx, professionalUserID, connectionID,
	); err != nil {
		return nil, err
	}
	stored, err := s.repository.ListCollectionRequestsForProfessional(
		ctx, professionalUserID, connectionID, listLimit,
	)
	if err != nil {
		return nil, err
	}
	return collectionRequestsFromStored(stored), nil
}

func (s *Service) ListCollectionRequestsForApp(
	ctx context.Context,
	appUserID string,
	connectionID string,
) ([]CollectionRequest, error) {
	if _, err := uuid.Parse(connectionID); err != nil {
		return nil, ErrInvalidInput
	}
	if err := s.repository.AppOwnsConnection(ctx, appUserID, connectionID); err != nil {
		return nil, err
	}
	stored, err := s.repository.ListCollectionRequestsForApp(
		ctx, appUserID, connectionID, listLimit,
	)
	if err != nil {
		return nil, err
	}
	return collectionRequestsFromStored(stored), nil
}

func (s *Service) DeclineCollectionRequest(
	ctx context.Context,
	appUserID string,
	requestID string,
) error {
	if _, err := uuid.Parse(requestID); err != nil {
		return ErrInvalidInput
	}
	return s.repository.DeclineCollectionRequest(
		ctx, appUserID, requestID, s.now().UTC(),
	)
}

// SendCollection é o único caminho pelo qual resposta de check-in chega a um
// profissional. O paciente escolhe quais check-ins entram; o retrato é
// calculado aqui e congelado, e o que o profissional lê depois é este
// arquivo, não a tabela viva.
func (s *Service) SendCollection(
	ctx context.Context,
	appUserID string,
	requestID string,
	assignmentIDs []string,
) (SendCollectionResult, error) {
	if _, err := uuid.Parse(requestID); err != nil {
		return SendCollectionResult{}, ErrInvalidInput
	}
	unique := make([]string, 0, len(assignmentIDs))
	for _, assignmentID := range assignmentIDs {
		if _, err := uuid.Parse(assignmentID); err != nil {
			return SendCollectionResult{}, ErrInvalidInput
		}
		if !slices.Contains(unique, assignmentID) {
			unique = append(unique, assignmentID)
		}
	}
	if len(unique) == 0 || len(unique) > MaxSharedCheckins {
		return SendCollectionResult{}, ErrInvalidInput
	}

	request, err := s.repository.CollectionRequestForApp(ctx, appUserID, requestID)
	if err != nil {
		return SendCollectionResult{}, err
	}
	if request.Status != "pending" {
		return SendCollectionResult{}, ErrRequestResolved
	}

	assignments, err := s.repository.AssignmentsForApp(ctx, appUserID, unique)
	if err != nil {
		return SendCollectionResult{}, err
	}
	if len(assignments) != len(unique) {
		return SendCollectionResult{}, ErrNotFound
	}

	checkins, answeredTotal, err := s.buildCollectionCheckins(
		ctx, assignments, request.PeriodStart, request.PeriodEnd,
	)
	if err != nil {
		return SendCollectionResult{}, err
	}
	if answeredTotal == 0 {
		return SendCollectionResult{}, ErrNoEntries
	}

	payload, err := json.Marshal(collectionPayload{Checkins: checkins})
	if err != nil {
		return SendCollectionResult{}, fmt.Errorf("encode check-in collection: %w", err)
	}
	ciphertext, err := s.cipher.Encrypt(payload)
	if err != nil {
		return SendCollectionResult{}, fmt.Errorf("encrypt check-in collection: %w", err)
	}

	if _, err := s.repository.CompleteCollectionRequest(
		ctx, appUserID, requestID, len(checkins), ciphertext, s.now().UTC(),
	); err != nil {
		return SendCollectionResult{}, err
	}
	return SendCollectionResult{
		RequestID: requestID, Status: "sent", CheckinCount: len(checkins),
	}, nil
}

func (s *Service) ListCollections(
	ctx context.Context,
	professionalUserID string,
	connectionID string,
) ([]Collection, error) {
	if _, err := uuid.Parse(connectionID); err != nil {
		return nil, ErrInvalidInput
	}
	access, err := s.authorizeProfessionalConnection(ctx, professionalUserID, connectionID)
	if err != nil {
		return nil, err
	}
	stored, err := s.repository.ListCollections(ctx, connectionID, listLimit)
	if err != nil {
		return nil, err
	}
	collections := make([]Collection, 0, len(stored))
	for _, item := range stored {
		plaintext, err := s.cipher.Decrypt(item.PayloadCiphertext)
		if err != nil {
			return nil, fmt.Errorf("decrypt check-in collection: %w", err)
		}
		var payload collectionPayload
		if err := json.Unmarshal(plaintext, &payload); err != nil {
			return nil, fmt.Errorf("decode check-in collection: %w", err)
		}
		checkins := make([]CollectionCheckin, 0, len(payload.Checkins))
		for _, stored := range payload.Checkins {
			checkin := stored.CollectionCheckin
			// Quem autorou um check-in de outro vínculo não é nomeado: a
			// existência de outro acompanhamento é informação do paciente,
			// e ele não foi perguntado sobre revelá-la.
			checkin.AuthoredByYou = stored.AuthoredByMembershipID == access.MembershipID
			checkins = append(checkins, checkin)
		}
		collections = append(collections, Collection{
			ID: item.ID, ConnectionID: item.ConnectionID, RequestID: item.RequestID,
			PeriodStart: formatDate(item.PeriodStart),
			PeriodEnd:   formatDate(item.PeriodEnd),
			SharedAt:    item.SharedAt, Checkins: checkins,
		})
	}
	return collections, nil
}

// buildCollectionCheckins é a conta inteira: média por pergunta, score do dia,
// melhor e pior dia. Fica no servidor de propósito — o app profissional
// recebe números prontos e não tem como derivar nada além do que foi
// autorizado.
func (s *Service) buildCollectionCheckins(
	ctx context.Context,
	assignments []storedAssignment,
	periodStart time.Time,
	periodEnd time.Time,
) ([]payloadCheckin, int, error) {
	templateIDs := make([]string, 0, len(assignments))
	assignmentIDs := make([]string, 0, len(assignments))
	for _, assignment := range assignments {
		templateIDs = append(templateIDs, assignment.TemplateID)
		assignmentIDs = append(assignmentIDs, assignment.ID)
	}

	questions, options, err := s.repository.LoadTemplateContent(ctx, templateIDs)
	if err != nil {
		return nil, 0, err
	}
	byTemplate, err := s.questionsByTemplate(questions, options)
	if err != nil {
		return nil, 0, err
	}
	headers, err := s.repository.TemplateHeaders(ctx, templateIDs)
	if err != nil {
		return nil, 0, err
	}
	entries, answers, err := s.repository.LoadEntries(
		ctx, assignmentIDs, periodStart, periodEnd,
	)
	if err != nil {
		return nil, 0, err
	}

	answersByEntry := make(map[string][]storedAnswer, len(entries))
	for _, answer := range answers {
		answersByEntry[answer.EntryID] = append(answersByEntry[answer.EntryID], answer)
	}
	entriesByAssignment := make(map[string][]storedEntry, len(assignments))
	for _, entry := range entries {
		entriesByAssignment[entry.AssignmentID] = append(
			entriesByAssignment[entry.AssignmentID], entry,
		)
	}

	periodDays := int(periodEnd.Sub(periodStart).Hours()/24) + 1
	answeredTotal := 0
	checkins := make([]payloadCheckin, 0, len(assignments))

	for _, assignment := range assignments {
		header, exists := headers[assignment.TemplateID]
		if !exists {
			return nil, 0, ErrNotFound
		}
		template, err := s.templateFromHeader(header, byTemplate[assignment.TemplateID])
		if err != nil {
			return nil, 0, err
		}
		aggregate := aggregateCheckin(
			template, entriesByAssignment[assignment.ID], answersByEntry, periodDays,
		)
		aggregate.AssignmentID = assignment.ID
		answeredTotal += aggregate.AnsweredDayCount
		checkins = append(checkins, payloadCheckin{
			CollectionCheckin:      aggregate,
			AuthoredByMembershipID: assignment.AuthoredByMembershipID,
		})
	}
	return checkins, answeredTotal, nil
}

// aggregateCheckin é a conta em si, sem banco: dado um check-in e os dias
// respondidos, produz média por pergunta, score por dia e os extremos. Fica
// separada para poder ser verificada com números na mão.
func aggregateCheckin(
	template Template,
	entries []storedEntry,
	answersByEntry map[string][]storedAnswer,
	periodDays int,
) CollectionCheckin {
	bounds := make(map[string]scoreBounds, len(template.Questions))
	for _, question := range template.Questions {
		bounds[question.ID] = boundsOf(question)
	}

	totals := make(map[string]*runningScore, len(template.Questions))
	overall := &runningScore{}
	days := make([]DayScore, 0, len(entries))

	for _, entry := range entries {
		day := &runningScore{}
		for _, answer := range answersByEntry[entry.ID] {
			bound, known := bounds[answer.QuestionID]
			if !known {
				continue
			}
			normalized := bound.normalize(answer.Score)
			day.add(float64(answer.Score), normalized)
			overall.add(float64(answer.Score), normalized)
			total, exists := totals[answer.QuestionID]
			if !exists {
				total = &runningScore{}
				totals[answer.QuestionID] = total
			}
			total.add(float64(answer.Score), normalized)
		}
		if day.Count == 0 {
			continue
		}
		days = append(days, DayScore{
			Date: formatDate(entry.EntryDate), Average: round2(day.average()),
			Normalized: round2(day.normalized()), AnswerCount: day.Count,
		})
	}

	questions := make([]QuestionAggregate, 0, len(template.Questions))
	for _, question := range template.Questions {
		bound := bounds[question.ID]
		aggregate := QuestionAggregate{
			QuestionID: question.ID, Prompt: question.Prompt,
			Position: question.Position, ScoreMin: bound.Min, ScoreMax: bound.Max,
		}
		if total, exists := totals[question.ID]; exists {
			aggregate.Average = round2(total.average())
			aggregate.Normalized = round2(total.normalized())
			aggregate.AnswerCount = total.Count
		}
		questions = append(questions, aggregate)
	}

	best, worst := extremes(days)
	return CollectionCheckin{
		Title: template.Title, Legend: template.Legend,
		PeriodDayCount: periodDays, AnsweredDayCount: len(days),
		Average: round2(overall.average()), Normalized: round2(overall.normalized()),
		Questions: questions, Days: days, BestDay: best, WorstDay: worst,
	}
}

type scoreBounds struct {
	Min int
	Max int
}

// normalize traz escalas diferentes à mesma régua. Sem isso, uma pergunta de
// 0–3 e outra de 0–5 dividiriam o mesmo radar mentindo sobre a proporção.
func (b scoreBounds) normalize(score int) float64 {
	if b.Max <= b.Min {
		return 0
	}
	return float64(score-b.Min) / float64(b.Max-b.Min)
}

func boundsOf(question Question) scoreBounds {
	bounds := scoreBounds{Min: MaxOptionScore, Max: MinOptionScore}
	for _, option := range question.Options {
		bounds.Min = min(bounds.Min, option.Score)
		bounds.Max = max(bounds.Max, option.Score)
	}
	return bounds
}

type runningScore struct {
	Raw        float64
	Normalized float64
	Count      int
}

func (r *runningScore) add(raw float64, normalized float64) {
	r.Raw += raw
	r.Normalized += normalized
	r.Count++
}

func (r *runningScore) average() float64 {
	if r.Count == 0 {
		return 0
	}
	return r.Raw / float64(r.Count)
}

func (r *runningScore) normalized() float64 {
	if r.Count == 0 {
		return 0
	}
	return r.Normalized / float64(r.Count)
}

// extremes devolve o melhor e o pior dia pela régua normalizada. Com um dia
// só, os dois apontam para o mesmo — e a UI precisa dizer isso, não fingir
// que houve variação.
func extremes(days []DayScore) (*DayScore, *DayScore) {
	if len(days) == 0 {
		return nil, nil
	}
	best, worst := days[0], days[0]
	for _, day := range days[1:] {
		if day.Normalized > best.Normalized {
			best = day
		}
		if day.Normalized < worst.Normalized {
			worst = day
		}
	}
	return &best, &worst
}

func collectionRequestFromStored(stored storedCollectionRequest) CollectionRequest {
	return CollectionRequest{
		ID: stored.ID, ConnectionID: stored.ConnectionID,
		ProfessionalDisplayName: stored.ProfessionalDisplayName,
		PatientDisplayName:      stored.PatientDisplayName,
		PeriodStart:             formatDate(stored.PeriodStart),
		PeriodEnd:               formatDate(stored.PeriodEnd),
		Status:                  stored.Status, RequestedAt: stored.RequestedAt,
		RespondedAt: stored.RespondedAt,
	}
}

func collectionRequestsFromStored(stored []storedCollectionRequest) []CollectionRequest {
	requests := make([]CollectionRequest, 0, len(stored))
	for _, request := range stored {
		requests = append(requests, collectionRequestFromStored(request))
	}
	return requests
}
