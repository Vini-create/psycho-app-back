package chat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Vini-create/psycho-app-back/internal/companion"
	"github.com/google/uuid"
)

const (
	maxConversationTitleRunes = 120
	maxUserMessageRunes       = 8000
	maxAssistantMessageRunes  = 12000
	maxHistoryRunes           = 60000
)

type Cipher interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
}

type ServiceConfig struct {
	ConsentPolicyVersion string
	HistoryMessages      int
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
		return nil, fmt.Errorf("chat service dependencies are required")
	}
	if strings.TrimSpace(config.ConsentPolicyVersion) == "" ||
		len(config.ConsentPolicyVersion) > 64 {
		return nil, fmt.Errorf("valid consent policy version is required")
	}
	if config.HistoryMessages < 1 || config.HistoryMessages > 50 {
		return nil, fmt.Errorf("history messages must be between 1 and 50")
	}
	return &Service{
		repository: repository,
		cipher:     cipher,
		companion:  companionClient,
		config:     config,
		now:        time.Now,
	}, nil
}

func (s *Service) GrantConsents(
	ctx context.Context,
	appUserID string,
	consentTypes []string,
	client ClientInfo,
) ([]Consent, error) {
	normalized, err := normalizeConsentTypes(consentTypes)
	if err != nil {
		return nil, err
	}
	if err := s.repository.GrantConsents(
		ctx,
		appUserID,
		normalized,
		s.config.ConsentPolicyVersion,
		client,
		s.now().UTC(),
	); err != nil {
		return nil, err
	}
	return s.repository.ListActiveConsents(ctx, appUserID)
}

func (s *Service) ListConsents(ctx context.Context, appUserID string) ([]Consent, error) {
	return s.repository.ListActiveConsents(ctx, appUserID)
}

func (s *Service) RevokeConsent(
	ctx context.Context,
	appUserID string,
	consentType string,
) error {
	if !validConsentType(consentType) {
		return ErrInvalidInput
	}
	return s.repository.RevokeConsent(ctx, appUserID, consentType, s.now().UTC())
}

func (s *Service) CreateConversation(
	ctx context.Context,
	appUserID string,
	title string,
) (Conversation, error) {
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		title = "Nova conversa"
	}
	if utf8.RuneCountInString(title) > maxConversationTitleRunes {
		return Conversation{}, ErrInvalidInput
	}
	ciphertext, err := s.cipher.Encrypt([]byte(title))
	if err != nil {
		return Conversation{}, fmt.Errorf("encrypt conversation title: %w", err)
	}
	stored, err := s.repository.CreateConversation(ctx, appUserID, ciphertext)
	if err != nil {
		return Conversation{}, err
	}
	return s.conversationFromStored(stored)
}

func (s *Service) ListConversations(
	ctx context.Context,
	appUserID string,
) ([]Conversation, error) {
	stored, err := s.repository.ListConversations(ctx, appUserID, 50)
	if err != nil {
		return nil, err
	}
	conversations := make([]Conversation, 0, len(stored))
	for _, item := range stored {
		conversation, err := s.conversationFromStored(item)
		if err != nil {
			return nil, err
		}
		conversations = append(conversations, conversation)
	}
	return conversations, nil
}

func (s *Service) RenameConversation(
	ctx context.Context,
	appUserID string,
	conversationID string,
	title string,
) (Conversation, error) {
	if _, err := uuid.Parse(conversationID); err != nil {
		return Conversation{}, ErrInvalidInput
	}
	title = strings.Join(strings.Fields(title), " ")
	if title == "" || utf8.RuneCountInString(title) > maxConversationTitleRunes {
		return Conversation{}, ErrInvalidInput
	}
	titleCiphertext, err := s.cipher.Encrypt([]byte(title))
	if err != nil {
		return Conversation{}, fmt.Errorf("encrypt conversation title: %w", err)
	}
	stored, err := s.repository.RenameConversation(
		ctx, appUserID, conversationID, titleCiphertext, s.now().UTC(),
	)
	if err != nil {
		return Conversation{}, err
	}
	return s.conversationFromStored(stored)
}

func (s *Service) ArchiveConversation(
	ctx context.Context,
	appUserID string,
	conversationID string,
) error {
	if _, err := uuid.Parse(conversationID); err != nil {
		return ErrInvalidInput
	}
	return s.repository.ArchiveConversation(ctx, appUserID, conversationID, s.now().UTC())
}

func (s *Service) ListMessages(
	ctx context.Context,
	appUserID string,
	conversationID string,
	beforeSequence int64,
	limit int,
) ([]Message, error) {
	if _, err := uuid.Parse(conversationID); err != nil || beforeSequence < 0 {
		return nil, ErrInvalidInput
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return nil, ErrInvalidInput
	}
	stored, err := s.repository.ListMessages(
		ctx, appUserID, conversationID, beforeSequence, limit,
	)
	if err != nil {
		return nil, err
	}
	return s.messagesFromStored(stored)
}

func (s *Service) SendMessage(
	ctx context.Context,
	appUserID string,
	conversationID string,
	content string,
	idempotencyKey string,
	localeHint string,
) (SendResult, error) {
	return s.sendMessage(ctx, appUserID, conversationID, content, idempotencyKey, localeHint, nil)
}

func (s *Service) SendMessageStream(
	ctx context.Context,
	appUserID string,
	conversationID string,
	content string,
	idempotencyKey string,
	localeHint string,
	onDelta func(string) error,
) (SendResult, error) {
	return s.sendMessage(
		ctx, appUserID, conversationID, content, idempotencyKey, localeHint, onDelta,
	)
}

func (s *Service) sendMessage(
	ctx context.Context,
	appUserID string,
	conversationID string,
	content string,
	idempotencyKey string,
	localeHint string,
	onDelta func(string) error,
) (SendResult, error) {
	if err := validateMessageInput(conversationID, content, idempotencyKey); err != nil {
		return SendResult{}, err
	}
	if err := s.requireCurrentConsents(ctx, appUserID); err != nil {
		return SendResult{}, err
	}

	content = strings.TrimSpace(content)
	ciphertext, err := s.cipher.Encrypt([]byte(content))
	if err != nil {
		return SendResult{}, fmt.Errorf("encrypt user message: %w", err)
	}
	requestHash := sha256.Sum256([]byte(idempotencyKey))
	stored, created, err := s.repository.AppendUserMessage(
		ctx,
		appUserID,
		conversationID,
		ciphertext,
		hex.EncodeToString(requestHash[:]),
		s.now().UTC(),
	)
	if err != nil {
		return SendResult{}, err
	}
	userMessage, err := s.messageFromStored(stored)
	if err != nil {
		return SendResult{}, err
	}
	if !created {
		return s.resultForExistingMessage(ctx, appUserID, userMessage)
	}

	return s.generateReply(ctx, appUserID, userMessage, localeHint, onDelta)
}

func (s *Service) RetryMessage(
	ctx context.Context,
	appUserID string,
	messageID string,
	localeHint string,
) (SendResult, error) {
	if _, err := uuid.Parse(messageID); err != nil {
		return SendResult{}, ErrInvalidInput
	}
	if err := s.requireCurrentConsents(ctx, appUserID); err != nil {
		return SendResult{}, err
	}
	stored, err := s.repository.FindUserMessage(ctx, appUserID, messageID)
	if err != nil {
		return SendResult{}, err
	}
	claimed, err := s.repository.ClaimGenerationRetry(
		ctx, appUserID, messageID, s.now().UTC(),
	)
	if err != nil {
		return SendResult{}, err
	}
	if !claimed {
		message, err := s.messageFromStored(stored)
		if err != nil {
			return SendResult{}, err
		}
		return s.resultForExistingMessage(ctx, appUserID, message)
	}
	stored.GenerationStatus = "pending"
	stored.FailureCode = nil
	message, err := s.messageFromStored(stored)
	if err != nil {
		return SendResult{}, err
	}
	return s.generateReply(ctx, appUserID, message, localeHint, nil)
}

func (s *Service) generateReply(
	ctx context.Context,
	appUserID string,
	userMessage Message,
	localeHint string,
	onDelta func(string) error,
) (SendResult, error) {
	historyStored, err := s.repository.ListMessages(
		ctx,
		appUserID,
		userMessage.ConversationID,
		userMessage.Sequence,
		s.config.HistoryMessages,
	)
	if err != nil {
		_ = s.markGenerationFailed(ctx, appUserID, userMessage.ID, "history_unavailable")
		return SendResult{}, err
	}
	historyMessages, err := s.messagesFromStored(historyStored)
	if err != nil {
		return SendResult{}, err
	}
	history := boundedCompanionHistory(historyMessages)

	request := companion.Request{
		RequestID:      userMessage.ID,
		ConversationID: userMessage.ConversationID,
		UserID:         appUserID,
		Message:        userMessage.Content,
		History:        history,
		LocaleHint:     strings.TrimSpace(localeHint),
	}
	var response companion.Response
	if onDelta != nil {
		if streaming, ok := s.companion.(companion.StreamingClient); ok {
			response, err = streaming.RespondStream(ctx, request, onDelta)
		} else {
			response, err = s.companion.Respond(ctx, request)
			if err == nil {
				err = onDelta(response.Content)
			}
		}
	} else {
		response, err = s.companion.Respond(ctx, request)
	}
	if err != nil {
		slog.Warn("companion request failed", "message_id", userMessage.ID, "error", err)
		if markErr := s.markGenerationFailed(
			ctx, appUserID, userMessage.ID, "companion_unavailable",
		); markErr != nil {
			return SendResult{}, markErr
		}
		userMessage.GenerationStatus = "failed"
		failureCode := "companion_unavailable"
		userMessage.FailureCode = &failureCode
		return SendResult{
			UserMessage: userMessage, AssistantStatus: "failed",
		}, nil
	}

	response.Content = strings.TrimSpace(response.Content)
	response.Provider = strings.TrimSpace(response.Provider)
	response.Model = strings.TrimSpace(response.Model)
	response.PromptVersion = strings.TrimSpace(response.PromptVersion)
	response.Language = strings.TrimSpace(response.Language)
	response.GraphVersion = strings.TrimSpace(response.GraphVersion)
	if utf8.RuneCountInString(response.Content) < 1 ||
		utf8.RuneCountInString(response.Content) > maxAssistantMessageRunes ||
		utf8.RuneCountInString(response.Provider) < 1 || len(response.Provider) > 100 ||
		utf8.RuneCountInString(response.Model) < 1 || len(response.Model) > 160 ||
		utf8.RuneCountInString(response.PromptVersion) < 1 || len(response.PromptVersion) > 100 ||
		utf8.RuneCountInString(response.Language) < 2 || len(response.Language) > 35 ||
		utf8.RuneCountInString(response.GraphVersion) < 1 || len(response.GraphVersion) > 100 ||
		!validCompanionRoute(response.Route) || (response.Route == "normal") == response.Blocked {
		if markErr := s.markGenerationFailed(
			ctx, appUserID, userMessage.ID, "invalid_companion_response",
		); markErr != nil {
			return SendResult{}, markErr
		}
		userMessage.GenerationStatus = "failed"
		failureCode := "invalid_companion_response"
		userMessage.FailureCode = &failureCode
		return SendResult{UserMessage: userMessage, AssistantStatus: "failed"}, nil
	}

	ciphertext, err := s.cipher.Encrypt([]byte(response.Content))
	if err != nil {
		_ = s.markGenerationFailed(ctx, appUserID, userMessage.ID, "encryption_failed")
		return SendResult{}, fmt.Errorf("encrypt assistant message: %w", err)
	}
	status := "completed"
	if response.Blocked {
		status = "blocked"
	}
	persistCtx, cancelPersist := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancelPersist()
	assistantStored, err := s.repository.CompleteGeneration(
		persistCtx,
		appUserID,
		userMessage.ID,
		ciphertext,
		status,
		response.Provider,
		response.Model,
		response.PromptVersion,
		s.now().UTC(),
	)
	if err != nil {
		return SendResult{}, err
	}
	assistantMessage, err := s.messageFromStored(assistantStored)
	if err != nil {
		return SendResult{}, err
	}
	userMessage.GenerationStatus = status
	return SendResult{
		UserMessage: userMessage, AssistantMessage: &assistantMessage,
		AssistantStatus: status,
	}, nil
}

func (s *Service) markGenerationFailed(
	ctx context.Context,
	appUserID string,
	messageID string,
	failureCode string,
) error {
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.repository.FailGeneration(persistCtx, appUserID, messageID, failureCode)
}

func (s *Service) resultForExistingMessage(
	ctx context.Context,
	appUserID string,
	userMessage Message,
) (SendResult, error) {
	storedReply, err := s.repository.FindReply(ctx, appUserID, userMessage.ID)
	if errors.Is(err, ErrNotFound) {
		return SendResult{
			UserMessage: userMessage, AssistantStatus: userMessage.GenerationStatus,
		}, nil
	}
	if err != nil {
		return SendResult{}, err
	}
	reply, err := s.messageFromStored(storedReply)
	if err != nil {
		return SendResult{}, err
	}
	return SendResult{
		UserMessage: userMessage, AssistantMessage: &reply,
		AssistantStatus: reply.GenerationStatus,
	}, nil
}

func (s *Service) requireCurrentConsents(ctx context.Context, appUserID string) error {
	for _, consentType := range []string{ConsentTerms, ConsentPrivacy, ConsentAIProcessing} {
		valid, err := s.repository.HasCurrentConsent(
			ctx, appUserID, consentType, s.config.ConsentPolicyVersion,
		)
		if err != nil {
			return err
		}
		if !valid {
			return ErrConsentRequired
		}
	}
	return nil
}

func (s *Service) conversationFromStored(stored storedConversation) (Conversation, error) {
	plaintext, err := s.cipher.Decrypt(stored.TitleCiphertext)
	if err != nil {
		return Conversation{}, fmt.Errorf("decrypt conversation title: %w", err)
	}
	return Conversation{
		ID: stored.ID, Title: string(plaintext), Status: stored.Status,
		LastMessageAt: stored.LastMessageAt, CreatedAt: stored.CreatedAt,
		UpdatedAt: stored.UpdatedAt,
	}, nil
}

func (s *Service) messageFromStored(stored storedMessage) (Message, error) {
	plaintext, err := s.cipher.Decrypt(stored.ContentCiphertext)
	if err != nil {
		return Message{}, fmt.Errorf("decrypt chat message: %w", err)
	}
	return Message{
		ID: stored.ID, ConversationID: stored.ConversationID,
		Sequence: stored.Sequence, Role: stored.Role, Content: string(plaintext),
		InReplyToMessageID: stored.InReplyToMessageID,
		GenerationStatus:   stored.GenerationStatus, AIProvider: stored.AIProvider,
		AIModel: stored.AIModel, PromptVersion: stored.PromptVersion,
		FailureCode: stored.FailureCode, CreatedAt: stored.CreatedAt,
	}, nil
}

func (s *Service) messagesFromStored(stored []storedMessage) ([]Message, error) {
	messages := make([]Message, 0, len(stored))
	for _, item := range stored {
		message, err := s.messageFromStored(item)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, nil
}

func boundedCompanionHistory(messages []Message) []companion.Message {
	history := make([]companion.Message, 0, len(messages))
	totalRunes := 0
	for index := len(messages) - 1; index >= 0; index-- {
		messageRunes := utf8.RuneCountInString(messages[index].Content)
		if totalRunes+messageRunes > maxHistoryRunes {
			break
		}
		totalRunes += messageRunes
		history = append(history, companion.Message{
			ID: messages[index].ID, Role: messages[index].Role,
			Content: messages[index].Content, CreatedAt: &messages[index].CreatedAt,
		})
	}
	for left, right := 0, len(history)-1; left < right; left, right = left+1, right-1 {
		history[left], history[right] = history[right], history[left]
	}
	return history
}

func validCompanionRoute(route string) bool {
	return route == "normal" || route == "boundary" || route == "crisis" ||
		route == "security_block"
}

func validateMessageInput(conversationID, content, idempotencyKey string) error {
	if _, err := uuid.Parse(conversationID); err != nil {
		return ErrInvalidInput
	}
	content = strings.TrimSpace(content)
	if !utf8.ValidString(content) || utf8.RuneCountInString(content) < 1 ||
		utf8.RuneCountInString(content) > maxUserMessageRunes {
		return ErrInvalidInput
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if len(idempotencyKey) < 8 || len(idempotencyKey) > 200 {
		return ErrInvalidInput
	}
	return nil
}

func normalizeConsentTypes(values []string) ([]string, error) {
	if len(values) == 0 || len(values) > 3 {
		return nil, ErrInvalidInput
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if !validConsentType(value) {
			return nil, ErrInvalidInput
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func validConsentType(value string) bool {
	return value == ConsentTerms || value == ConsentPrivacy || value == ConsentAIProcessing
}
