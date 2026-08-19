package insight

import (
	"context"
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
			ID: stored.ID, Role: stored.Role, Content: string(plaintext),
			CreatedAt: stored.CreatedAt,
		})
		allowedSources[stored.ID] = struct{}{}
	}

	response, err := s.companion.ProcessContext(ctx, companion.ContextRequest{
		RequestID: job.ID, ConnectionID: job.ConnectionID, UserID: access.AppUserID,
		PeriodStart: job.PeriodStart, PeriodEnd: job.PeriodEnd, Messages: messages,
	})
	if err != nil {
		slog.Warn("context processing failed", "job_id", job.ID, "error", err)
		return s.retryOrFail(ctx, job, "companion_unavailable")
	}

	summaryText := strings.TrimSpace(response.Summary)
	provider := strings.TrimSpace(response.Provider)
	model := strings.TrimSpace(response.Model)
	promptVersion := strings.TrimSpace(response.PromptVersion)
	if utf8.RuneCountInString(summaryText) < 1 || utf8.RuneCountInString(summaryText) > 12000 ||
		utf8.RuneCountInString(provider) < 1 || utf8.RuneCountInString(provider) > 100 ||
		utf8.RuneCountInString(model) < 1 || utf8.RuneCountInString(model) > 160 ||
		utf8.RuneCountInString(promptVersion) < 1 || utf8.RuneCountInString(promptVersion) > 100 ||
		len(response.Items) > 100 {
		return s.failJob(ctx, job.ID, "invalid_companion_response")
	}

	summaryCiphertext, err := s.cipher.Encrypt([]byte(summaryText))
	if err != nil {
		return s.failJob(ctx, job.ID, "encryption_failed")
	}
	items := make([]itemWrite, 0, len(response.Items))
	for _, input := range response.Items {
		if !validItemKind(input.Kind) || strings.TrimSpace(input.Description) == "" ||
			utf8.RuneCountInString(input.Description) > 4000 || len(input.SourceMessageIDs) == 0 ||
			(input.Confidence != nil && (*input.Confidence < 0 || *input.Confidence > 1)) {
			return s.failJob(ctx, job.ID, "invalid_companion_response")
		}
		for _, sourceID := range input.SourceMessageIDs {
			if _, allowed := allowedSources[sourceID]; !allowed {
				return s.failJob(ctx, job.ID, "invalid_source_reference")
			}
		}
		descriptionCiphertext, err := s.cipher.Encrypt(
			[]byte(strings.TrimSpace(input.Description)),
		)
		if err != nil {
			return s.failJob(ctx, job.ID, "encryption_failed")
		}
		items = append(items, itemWrite{
			Kind: input.Kind, DescriptionCiphertext: descriptionCiphertext,
			Confidence: input.Confidence, OccurredAt: input.OccurredAt,
			SourceMessageIDs: input.SourceMessageIDs,
		})
	}

	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
	defer cancel()
	_, err = s.repository.CompleteJob(
		persistCtx, job.ID, job.ConnectionID, job.PeriodStart, job.PeriodEnd,
		summaryCiphertext, provider, model, promptVersion,
		items, s.now().UTC(),
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
		plaintext, err := s.cipher.Decrypt(stored.SummaryCiphertext)
		if err != nil {
			return nil, fmt.Errorf("decrypt context summary: %w", err)
		}
		storedItems, err := s.repository.ListItems(ctx, stored.ID)
		if err != nil {
			return nil, err
		}
		items := make([]Item, 0, len(storedItems))
		for _, storedItem := range storedItems {
			if !scopeAllowsItem(access.Scopes, storedItem.Kind) {
				continue
			}
			description, err := s.cipher.Decrypt(storedItem.DescriptionCiphertext)
			if err != nil {
				return nil, fmt.Errorf("decrypt context item: %w", err)
			}
			items = append(items, Item{
				ID: storedItem.ID, Kind: storedItem.Kind, Description: string(description),
				Confidence: storedItem.Confidence, OccurredAt: storedItem.OccurredAt,
			})
		}
		summaries = append(summaries, Summary{
			ID: stored.ID, ConnectionID: stored.ConnectionID,
			PeriodStart: stored.PeriodStart, PeriodEnd: stored.PeriodEnd,
			Summary: string(plaintext), Items: items, Provider: stored.Provider,
			Model: stored.Model, PromptVersion: stored.PromptVersion,
			CreatedAt: stored.CreatedAt,
		})
	}
	return summaries, nil
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
	return kind == "event" || kind == "theme" || kind == "marked_topic"
}

func scopeAllowsItem(scopes []string, kind string) bool {
	switch kind {
	case "theme":
		return slices.Contains(scopes, "summaries")
	case "event":
		return slices.Contains(scopes, "events")
	case "marked_topic":
		return slices.Contains(scopes, "marked_topics")
	default:
		return false
	}
}
