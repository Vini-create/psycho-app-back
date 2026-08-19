package insight

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Vini-create/psycho-app-back/internal/companion"
	"github.com/google/uuid"
)

const maxSourceMessages = 500

type Cipher interface {
	Encrypt([]byte) ([]byte, error)
	Decrypt([]byte) ([]byte, error)
}

type ServiceConfig struct {
	ConsentPolicyVersion string
	WorkerLease          time.Duration
	MaxAttempts          int
}

type Service struct {
	repository *Repository
	cipher     Cipher
	companion  companion.Client
	config     ServiceConfig
	now        func() time.Time
}

func NewService(
	repository *Repository,
	cipher Cipher,
	companionClient companion.Client,
	config ServiceConfig,
) (*Service, error) {
	if repository == nil || cipher == nil || companionClient == nil {
		return nil, fmt.Errorf("insight service dependencies are required")
	}
	if strings.TrimSpace(config.ConsentPolicyVersion) == "" {
		return nil, fmt.Errorf("consent policy version is required")
	}
	if config.WorkerLease == 0 {
		config.WorkerLease = 2 * time.Minute
	}
	if config.MaxAttempts == 0 {
		config.MaxAttempts = 3
	}
	if config.WorkerLease < 10*time.Second || config.MaxAttempts < 1 || config.MaxAttempts > 10 {
		return nil, fmt.Errorf("valid insight worker configuration is required")
	}
	return &Service{
		repository: repository, cipher: cipher, companion: companionClient,
		config: config, now: time.Now,
	}, nil
}

func (s *Service) Generate(
	ctx context.Context,
	professionalUserID string,
	connectionID string,
	periodStart time.Time,
	periodEnd time.Time,
) (GenerationResult, error) {
	if _, err := uuid.Parse(connectionID); err != nil {
		return GenerationResult{}, ErrInvalidInput
	}
	periodStart = periodStart.UTC()
	periodEnd = periodEnd.UTC()
	now := s.now().UTC()
	if periodStart.IsZero() || periodEnd.IsZero() || !periodEnd.After(periodStart) ||
		periodEnd.Sub(periodStart) > 31*24*time.Hour || periodEnd.After(now.Add(time.Minute)) {
		return GenerationResult{}, ErrInvalidInput
	}

	access, err := s.repository.ProfessionalConnectionAccess(
		ctx, professionalUserID, connectionID, s.config.ConsentPolicyVersion,
	)
	if err != nil {
		return GenerationResult{}, err
	}
	if !slices.Contains(access.Scopes, "summaries") {
		return GenerationResult{}, ErrForbidden
	}
	if periodStart.Before(access.ActivatedAt) {
		return GenerationResult{}, ErrForbidden
	}

	jobID, err := s.repository.CreateJob(
		ctx, connectionID, professionalUserID, periodStart, periodEnd, now,
	)
	if err != nil {
		return GenerationResult{}, err
	}
	return GenerationResult{JobID: jobID, Status: "queued"}, nil
}

func (s *Service) GetJob(
	ctx context.Context,
	professionalUserID string,
	jobID string,
) (Job, error) {
	if _, err := uuid.Parse(jobID); err != nil {
		return Job{}, ErrInvalidInput
	}
	stored, err := s.repository.GetJob(ctx, professionalUserID, jobID)
	if err != nil {
		return Job{}, err
	}
	return jobFromStored(stored), nil
}

func (s *Service) ProcessNext(ctx context.Context) (bool, error) {
	now := s.now().UTC()
	job, claimed, err := s.repository.ClaimNextJob(
		ctx, now, now.Add(-s.config.WorkerLease),
	)
	if err != nil || !claimed {
		return claimed, err
	}
	return true, s.processJob(ctx, job)
}

func (s *Service) processJob(ctx context.Context, job storedJob) error {
	access, err := s.repository.ProfessionalConnectionAccess(
		ctx, job.ProfessionalUserID, job.ConnectionID, s.config.ConsentPolicyVersion,
	)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			return s.retryOrFail(ctx, job, "access_query_failed")
		}
		return s.failJob(ctx, job.ID, "access_revoked")
	}
	if !slices.Contains(access.Scopes, "summaries") || job.PeriodStart.Before(access.ActivatedAt) {
		if failErr := s.failJob(ctx, job.ID, "access_revoked"); failErr != nil {
			return failErr
		}
		return nil
	}

	storedMessages, err := s.repository.LoadSourceMessages(
		ctx, access.AppUserID, job.PeriodStart, job.PeriodEnd, maxSourceMessages+1,
	)
	if err != nil {
		return s.retryOrFail(ctx, job, "source_query_failed")
	}
	if len(storedMessages) == 0 {
		return s.failJob(ctx, job.ID, "no_messages")
	}
	if len(storedMessages) > maxSourceMessages {
		return s.failJob(ctx, job.ID, "period_too_large")
	}

	messages := make([]companion.ContextMessage, 0, len(storedMessages))
	allowedSources := make(map[string]struct{}, len(storedMessages))
	for _, stored := range storedMessages {
		plaintext, err := s.cipher.Decrypt(stored.ContentCiphertext)
		if err != nil {
			return s.failJob(ctx, job.ID, "decryption_failed")
		}
		messages = append(messages, companion.ContextMessage{
			ID: stored.ID, ConversationID: stored.ConversationID,
			Role: stored.Role, Content: string(plaintext),
			CreatedAt: stored.CreatedAt,
		})
		if stored.Role == "user" {
			allowedSources[stored.ID] = struct{}{}
		}
	}

	response, err := s.companion.ProcessContext(ctx, companion.ContextRequest{
		RequestID: job.ID, ConnectionID: job.ConnectionID, UserID: access.AppUserID,
		PeriodStart: job.PeriodStart, PeriodEnd: job.PeriodEnd, Messages: messages,
	})
	if err != nil {
		slog.Warn("context processing failed", "job_id", job.ID, "error", err)
		return s.retryOrFail(ctx, job, "companion_unavailable")
	}

	schemaVersion := strings.TrimSpace(response.SchemaVersion)
	title := strings.TrimSpace(response.Title)
	summaryText := strings.TrimSpace(response.Summary)
	provider := strings.TrimSpace(response.Provider)
	model := strings.TrimSpace(response.Model)
	promptVersion := strings.TrimSpace(response.PromptVersion)
	graphVersion := strings.TrimSpace(response.GraphVersion)
	coverageNote := strings.TrimSpace(response.Coverage.Note)
	if utf8.RuneCountInString(schemaVersion) < 1 || utf8.RuneCountInString(schemaVersion) > 100 ||
		utf8.RuneCountInString(title) < 1 || utf8.RuneCountInString(title) > 240 ||
		utf8.RuneCountInString(summaryText) < 1 || utf8.RuneCountInString(summaryText) > 12000 ||
		utf8.RuneCountInString(provider) < 1 || utf8.RuneCountInString(provider) > 100 ||
		utf8.RuneCountInString(model) < 1 || utf8.RuneCountInString(model) > 160 ||
		utf8.RuneCountInString(promptVersion) < 1 || utf8.RuneCountInString(promptVersion) > 100 ||
		utf8.RuneCountInString(graphVersion) < 1 || utf8.RuneCountInString(graphVersion) > 100 ||
		utf8.RuneCountInString(coverageNote) < 1 || utf8.RuneCountInString(coverageNote) > 1000 ||
		response.Coverage.ConversationCount < 1 || response.Coverage.ConversationCount > 500 ||
		response.Coverage.UserMessageCount < 1 || response.Coverage.UserMessageCount > 500 ||
		response.Coverage.ActiveDayCount < 1 || response.Coverage.ActiveDayCount > 31 ||
		!validCompleteness(response.Coverage.Completeness) || len(response.Items) > 100 ||
		len(response.Timeline) > 100 || len(response.Limitations) > 20 {
		return s.failJob(ctx, job.ID, "invalid_companion_response")
	}

	titleCiphertext, err := s.encryptRequired(title)
	if err != nil {
		return s.failJob(ctx, job.ID, "encryption_failed")
	}
	coverageNoteCiphertext, err := s.encryptRequired(coverageNote)
	if err != nil {
		return s.failJob(ctx, job.ID, "encryption_failed")
	}
	summaryCiphertext, err := s.encryptRequired(summaryText)
	if err != nil {
		return s.failJob(ctx, job.ID, "encryption_failed")
	}
	limitationsCiphertext, err := s.encryptStrings(response.Limitations, 1000)
	if err != nil {
		return s.failJob(ctx, job.ID, "invalid_companion_response")
	}

	timeline := make([]timelineWrite, 0, len(response.Timeline))
	for _, input := range response.Timeline {
		description := strings.TrimSpace(input.Description)
		if utf8.RuneCountInString(description) < 1 || utf8.RuneCountInString(description) > 2000 ||
			len(input.SourceMessageIDs) < 1 || len(input.SourceMessageIDs) > 50 ||
			!validSourceIDs(input.SourceMessageIDs, allowedSources) ||
			!timestampWithinPeriod(input.OccurredAt, job.PeriodStart, job.PeriodEnd) {
			return s.failJob(ctx, job.ID, "invalid_companion_response")
		}
		descriptionCiphertext, err := s.encryptRequired(description)
		if err != nil {
			return s.failJob(ctx, job.ID, "encryption_failed")
		}
		timeline = append(timeline, timelineWrite{
			DescriptionCiphertext: descriptionCiphertext,
			OccurredAt:            input.OccurredAt, SourceMessageIDs: input.SourceMessageIDs,
		})
	}

	items := make([]itemWrite, 0, len(response.Items))
	for _, input := range response.Items {
		title := strings.TrimSpace(input.Title)
		description := strings.TrimSpace(input.Description)
		impact := strings.TrimSpace(input.Impact)
		if !validItemKind(input.Kind) || !validEvidenceStrength(input.EvidenceStrength) ||
			utf8.RuneCountInString(title) < 1 || utf8.RuneCountInString(title) > 240 ||
			utf8.RuneCountInString(description) < 1 || utf8.RuneCountInString(description) > 4000 ||
			utf8.RuneCountInString(impact) > 2000 || len(input.Limitations) > 10 ||
			len(input.SourceMessageIDs) < 1 || len(input.SourceMessageIDs) > 50 ||
			!validSourceIDs(input.SourceMessageIDs, allowedSources) ||
			!timestampWithinPeriod(input.OccurredAt, job.PeriodStart, job.PeriodEnd) {
			return s.failJob(ctx, job.ID, "invalid_companion_response")
		}
		titleCiphertext, err := s.encryptRequired(title)
		if err != nil {
			return s.failJob(ctx, job.ID, "encryption_failed")
		}
		descriptionCiphertext, err := s.encryptRequired(description)
		if err != nil {
			return s.failJob(ctx, job.ID, "encryption_failed")
		}
		impactCiphertext, err := s.encryptOptional(impact)
		if err != nil {
			return s.failJob(ctx, job.ID, "encryption_failed")
		}
		itemLimitationsCiphertext, err := s.encryptStrings(input.Limitations, 1000)
		if err != nil {
			return s.failJob(ctx, job.ID, "invalid_companion_response")
		}
		items = append(items, itemWrite{
			Kind: input.Kind, TitleCiphertext: titleCiphertext,
			DescriptionCiphertext: descriptionCiphertext, ImpactCiphertext: impactCiphertext,
			EvidenceStrength: input.EvidenceStrength, OccurredAt: input.OccurredAt,
			LimitationsCiphertext: itemLimitationsCiphertext,
			SourceMessageIDs:      input.SourceMessageIDs,
		})
	}

	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
	defer cancel()
	_, err = s.repository.CompleteJob(
		persistCtx, job.ID, job.ConnectionID, job.PeriodStart, job.PeriodEnd,
		reportWrite{
			SchemaVersion: schemaVersion, TitleCiphertext: titleCiphertext,
			CoverageConversationCount: response.Coverage.ConversationCount,
			CoverageUserMessageCount:  response.Coverage.UserMessageCount,
			CoverageActiveDayCount:    response.Coverage.ActiveDayCount,
			CoverageCompleteness:      response.Coverage.Completeness,
			CoverageNoteCiphertext:    coverageNoteCiphertext,
			SummaryCiphertext:         summaryCiphertext, LimitationsCiphertext: limitationsCiphertext,
			Provider: provider, Model: model, PromptVersion: promptVersion,
			GraphVersion: graphVersion, Timeline: timeline, Items: items,
		}, s.now().UTC(),
	)
	if err != nil {
		return err
	}
	return nil
}

func (s *Service) List(
	ctx context.Context,
	professionalUserID string,
	connectionID string,
) ([]Summary, error) {
	if _, err := uuid.Parse(connectionID); err != nil {
		return nil, ErrInvalidInput
	}
	access, err := s.repository.ProfessionalConnectionAccess(
		ctx, professionalUserID, connectionID, s.config.ConsentPolicyVersion,
	)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(access.Scopes, "summaries") {
		return nil, ErrForbidden
	}
	storedSummaries, err := s.repository.ListSummaries(ctx, connectionID, 50)
	if err != nil {
		return nil, err
	}
	summaries := make([]Summary, 0, len(storedSummaries))
	for _, stored := range storedSummaries {
		summary, err := s.summaryFromStored(ctx, stored, access.Scopes)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}

func (s *Service) ListForApp(ctx context.Context, appUserID string) ([]Summary, error) {
	storedSummaries, err := s.repository.ListSummariesForApp(ctx, appUserID, 50)
	if err != nil {
		return nil, err
	}
	summaries := make([]Summary, 0, len(storedSummaries))
	for _, stored := range storedSummaries {
		summary, err := s.summaryFromStored(ctx, stored, []string{"summaries"})
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}

func (s *Service) Review(
	ctx context.Context,
	appUserID string,
	summaryID string,
	decision string,
	excludedItemIDs []string,
	excludedTimelineEntryIDs []string,
) error {
	if _, err := uuid.Parse(summaryID); err != nil ||
		(decision != "approved" && decision != "rejected") ||
		len(excludedItemIDs) > 100 || len(excludedTimelineEntryIDs) > 100 ||
		(decision == "rejected" && (len(excludedItemIDs) > 0 || len(excludedTimelineEntryIDs) > 0)) {
		return ErrInvalidInput
	}
	seen := make(map[string]struct{}, len(excludedItemIDs))
	for _, itemID := range excludedItemIDs {
		if _, err := uuid.Parse(itemID); err != nil {
			return ErrInvalidInput
		}
		if _, exists := seen[itemID]; exists {
			return ErrInvalidInput
		}
		seen[itemID] = struct{}{}
	}
	seenTimeline := make(map[string]struct{}, len(excludedTimelineEntryIDs))
	for _, entryID := range excludedTimelineEntryIDs {
		if _, err := uuid.Parse(entryID); err != nil {
			return ErrInvalidInput
		}
		if _, exists := seenTimeline[entryID]; exists {
			return ErrInvalidInput
		}
		seenTimeline[entryID] = struct{}{}
	}
	return s.repository.ReviewSummary(
		ctx, appUserID, summaryID, decision, excludedItemIDs,
		excludedTimelineEntryIDs, s.now().UTC(),
	)
}

func (s *Service) summaryFromStored(
	ctx context.Context,
	stored storedSummary,
	scopes []string,
) (Summary, error) {
	title, err := s.cipher.Decrypt(stored.TitleCiphertext)
	if err != nil {
		return Summary{}, fmt.Errorf("decrypt context title: %w", err)
	}
	coverageNote, err := s.cipher.Decrypt(stored.CoverageNoteCiphertext)
	if err != nil {
		return Summary{}, fmt.Errorf("decrypt context coverage note: %w", err)
	}
	summaryText, err := s.cipher.Decrypt(stored.SummaryCiphertext)
	if err != nil {
		return Summary{}, fmt.Errorf("decrypt context summary: %w", err)
	}
	limitations, err := s.decryptStrings(stored.LimitationsCiphertext)
	if err != nil {
		return Summary{}, fmt.Errorf("decrypt context limitations: %w", err)
	}

	storedTimeline, err := s.repository.ListTimeline(ctx, stored.ID)
	if err != nil {
		return Summary{}, err
	}
	timeline := make([]TimelineEntry, 0, len(storedTimeline))
	for _, storedEntry := range storedTimeline {
		description, err := s.cipher.Decrypt(storedEntry.DescriptionCiphertext)
		if err != nil {
			return Summary{}, fmt.Errorf("decrypt context timeline: %w", err)
		}
		timeline = append(timeline, TimelineEntry{
			ID: storedEntry.ID, Description: string(description),
			OccurredAt: storedEntry.OccurredAt,
		})
	}

	storedItems, err := s.repository.ListItems(ctx, stored.ID)
	if err != nil {
		return Summary{}, err
	}
	items := make([]Item, 0, len(storedItems))
	for _, storedItem := range storedItems {
		if !scopeAllowsItem(scopes, storedItem.Kind) {
			continue
		}
		title, err := s.cipher.Decrypt(storedItem.TitleCiphertext)
		if err != nil {
			return Summary{}, fmt.Errorf("decrypt context item title: %w", err)
		}
		description, err := s.cipher.Decrypt(storedItem.DescriptionCiphertext)
		if err != nil {
			return Summary{}, fmt.Errorf("decrypt context item: %w", err)
		}
		impact, err := s.decryptOptional(storedItem.ImpactCiphertext)
		if err != nil {
			return Summary{}, fmt.Errorf("decrypt context item impact: %w", err)
		}
		itemLimitations, err := s.decryptStrings(storedItem.LimitationsCiphertext)
		if err != nil {
			return Summary{}, fmt.Errorf("decrypt context item limitations: %w", err)
		}
		items = append(items, Item{
			ID: storedItem.ID, Kind: storedItem.Kind, Title: string(title),
			Description: string(description), Impact: impact,
			EvidenceStrength: storedItem.EvidenceStrength,
			OccurredAt:       storedItem.OccurredAt, Limitations: itemLimitations,
			Included: storedItem.Included,
		})
	}

	return Summary{
		ID: stored.ID, ConnectionID: stored.ConnectionID,
		SchemaVersion: stored.SchemaVersion, Title: string(title),
		PeriodStart: stored.PeriodStart, PeriodEnd: stored.PeriodEnd,
		Coverage: Coverage{
			ConversationCount: stored.CoverageConversationCount,
			UserMessageCount:  stored.CoverageUserMessageCount,
			ActiveDayCount:    stored.CoverageActiveDayCount,
			Completeness:      stored.CoverageCompleteness, Note: string(coverageNote),
		},
		Summary: string(summaryText), Timeline: timeline, Items: items,
		Limitations: limitations, Provider: stored.Provider, Model: stored.Model,
		PromptVersion: stored.PromptVersion, GraphVersion: stored.GraphVersion,
		ReviewStatus: stored.ReviewStatus, ReviewedAt: stored.ReviewedAt,
		CreatedAt: stored.CreatedAt,
	}, nil
}

func (s *Service) failJob(ctx context.Context, jobID, failureCode string) error {
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.repository.FailJob(persistCtx, jobID, failureCode, s.now().UTC())
}

func (s *Service) retryOrFail(ctx context.Context, job storedJob, failureCode string) error {
	if job.AttemptCount >= s.config.MaxAttempts {
		return s.failJob(ctx, job.ID, failureCode)
	}
	delay := time.Duration(job.AttemptCount*job.AttemptCount) * 5 * time.Second
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.repository.RetryJob(
		persistCtx, job.ID, failureCode, s.now().UTC().Add(delay), s.now().UTC(),
	)
}

func jobFromStored(stored storedJob) Job {
	return Job{
		ID: stored.ID, ConnectionID: stored.ConnectionID,
		PeriodStart: stored.PeriodStart, PeriodEnd: stored.PeriodEnd,
		Status: stored.Status, AttemptCount: stored.AttemptCount,
		CompletedAt: stored.CompletedAt, CreatedAt: stored.CreatedAt,
		UpdatedAt: stored.UpdatedAt,
	}
}

func validItemKind(kind string) bool {
	return slices.Contains([]string{
		"priority", "event", "challenge", "emotion", "thought", "behavior",
		"strategy", "support", "change", "open_topic", "safety_context",
	}, kind)
}

func scopeAllowsItem(scopes []string, kind string) bool {
	return validItemKind(kind) && slices.Contains(scopes, "summaries")
}

func validEvidenceStrength(value string) bool {
	return value == "explicit_once" || value == "explicit_repeated" ||
		value == "uncertain" || value == "contradictory"
}

func validCompleteness(value string) bool {
	return value == "limited" || value == "partial" || value == "substantial"
}

func validSourceIDs(sourceIDs []string, allowed map[string]struct{}) bool {
	seen := make(map[string]struct{}, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		if _, exists := seen[sourceID]; exists {
			return false
		}
		if _, exists := allowed[sourceID]; !exists {
			return false
		}
		seen[sourceID] = struct{}{}
	}
	return true
}

func timestampWithinPeriod(value *time.Time, start time.Time, end time.Time) bool {
	return value == nil || (!value.Before(start) && value.Before(end))
}

func (s *Service) encryptRequired(value string) ([]byte, error) {
	return s.cipher.Encrypt([]byte(value))
}

func (s *Service) encryptOptional(value string) ([]byte, error) {
	if value == "" {
		return nil, nil
	}
	return s.encryptRequired(value)
}

func (s *Service) encryptStrings(values []string, maximumRunes int) ([]byte, error) {
	if len(values) == 0 {
		return nil, nil
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" || utf8.RuneCountInString(value) > maximumRunes {
			return nil, ErrInvalidInput
		}
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("encode encrypted strings: %w", err)
	}
	return s.cipher.Encrypt(encoded)
}

func (s *Service) decryptOptional(ciphertext []byte) (string, error) {
	if len(ciphertext) == 0 {
		return "", nil
	}
	plaintext, err := s.cipher.Decrypt(ciphertext)
	return string(plaintext), err
}

func (s *Service) decryptStrings(ciphertext []byte) ([]string, error) {
	if len(ciphertext) == 0 {
		return []string{}, nil
	}
	plaintext, err := s.cipher.Decrypt(ciphertext)
	if err != nil {
		return nil, err
	}
	var values []string
	if err := json.Unmarshal(plaintext, &values); err != nil {
		return nil, fmt.Errorf("decode encrypted strings: %w", err)
	}
	return values, nil
}
