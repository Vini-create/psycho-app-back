package insight

import (
	"context"
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

	storedMessages, err := s.repository.LoadSourceMessages(
		ctx, access.AppUserID, periodStart, periodEnd, maxSourceMessages+1,
	)
	if err != nil {
		_ = s.failJob(ctx, jobID, "source_query_failed")
		return GenerationResult{}, err
	}
	if len(storedMessages) == 0 {
		_ = s.failJob(ctx, jobID, "no_messages")
		return GenerationResult{}, ErrNoMessages
	}
	if len(storedMessages) > maxSourceMessages {
		_ = s.failJob(ctx, jobID, "period_too_large")
		return GenerationResult{}, ErrPeriodTooLarge
	}

	messages := make([]companion.ContextMessage, 0, len(storedMessages))
	allowedSources := make(map[string]struct{}, len(storedMessages))
	for _, stored := range storedMessages {
		plaintext, err := s.cipher.Decrypt(stored.ContentCiphertext)
		if err != nil {
			_ = s.failJob(ctx, jobID, "decryption_failed")
			return GenerationResult{}, fmt.Errorf("decrypt context source: %w", err)
		}
		messages = append(messages, companion.ContextMessage{
			ID: stored.ID, Role: stored.Role, Content: string(plaintext),
			CreatedAt: stored.CreatedAt,
		})
		allowedSources[stored.ID] = struct{}{}
	}

	response, err := s.companion.ProcessContext(ctx, companion.ContextRequest{
		RequestID: jobID, ConnectionID: connectionID, UserID: access.AppUserID,
		PeriodStart: periodStart, PeriodEnd: periodEnd, Messages: messages,
	})
	if err != nil {
		slog.Warn("context processing failed", "job_id", jobID, "error", err)
		if failErr := s.failJob(ctx, jobID, "companion_unavailable"); failErr != nil {
			return GenerationResult{}, failErr
		}
		return GenerationResult{JobID: jobID, Status: "failed"}, nil
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
		_ = s.failJob(ctx, jobID, "invalid_companion_response")
		return GenerationResult{JobID: jobID, Status: "failed"}, nil
	}

	summaryCiphertext, err := s.cipher.Encrypt([]byte(summaryText))
	if err != nil {
		_ = s.failJob(ctx, jobID, "encryption_failed")
		return GenerationResult{}, err
	}
	items := make([]itemWrite, 0, len(response.Items))
	for _, input := range response.Items {
		if !validItemKind(input.Kind) || strings.TrimSpace(input.Description) == "" ||
			utf8.RuneCountInString(input.Description) > 4000 || len(input.SourceMessageIDs) == 0 ||
			(input.Confidence != nil && (*input.Confidence < 0 || *input.Confidence > 1)) {
			_ = s.failJob(ctx, jobID, "invalid_companion_response")
			return GenerationResult{JobID: jobID, Status: "failed"}, nil
		}
		for _, sourceID := range input.SourceMessageIDs {
			if _, allowed := allowedSources[sourceID]; !allowed {
				_ = s.failJob(ctx, jobID, "invalid_source_reference")
				return GenerationResult{JobID: jobID, Status: "failed"}, nil
			}
		}
		descriptionCiphertext, err := s.cipher.Encrypt(
			[]byte(strings.TrimSpace(input.Description)),
		)
		if err != nil {
			_ = s.failJob(ctx, jobID, "encryption_failed")
			return GenerationResult{}, err
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
		persistCtx, jobID, connectionID, periodStart, periodEnd,
		summaryCiphertext, provider, model, promptVersion,
		items, s.now().UTC(),
	)
	if err != nil {
		return GenerationResult{}, err
	}

	summaries, err := s.List(persistCtx, professionalUserID, connectionID)
	if err != nil || len(summaries) == 0 {
		return GenerationResult{}, err
	}
	return GenerationResult{JobID: jobID, Status: "completed", Summary: &summaries[0]}, nil
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
