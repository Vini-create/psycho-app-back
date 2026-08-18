package chat

import (
	"errors"
	"time"
)

var (
	ErrNotFound        = errors.New("resource not found")
	ErrForbidden       = errors.New("operation is forbidden")
	ErrInvalidInput    = errors.New("invalid input")
	ErrConflict        = errors.New("resource state conflict")
	ErrConsentRequired = errors.New("AI processing consent is required")
)

const (
	ConsentTerms        = "terms"
	ConsentPrivacy      = "privacy"
	ConsentAIProcessing = "ai_processing"
)

type ClientInfo struct {
	IPAddress string
	UserAgent string
}

type Consent struct {
	Type          string    `json:"type"`
	PolicyVersion string    `json:"policy_version"`
	GrantedAt     time.Time `json:"granted_at"`
}

type Conversation struct {
	ID            string     `json:"id"`
	Title         string     `json:"title"`
	Status        string     `json:"status"`
	LastMessageAt *time.Time `json:"last_message_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type Message struct {
	ID                 string    `json:"id"`
	ConversationID     string    `json:"conversation_id"`
	Sequence           int64     `json:"sequence"`
	Role               string    `json:"role"`
	Content            string    `json:"content"`
	InReplyToMessageID *string   `json:"in_reply_to_message_id,omitempty"`
	GenerationStatus   string    `json:"generation_status"`
	AIProvider         *string   `json:"ai_provider,omitempty"`
	AIModel            *string   `json:"ai_model,omitempty"`
	PromptVersion      *string   `json:"prompt_version,omitempty"`
	FailureCode        *string   `json:"failure_code,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
}

type SendResult struct {
	UserMessage      Message  `json:"user_message"`
	AssistantMessage *Message `json:"assistant_message,omitempty"`
	AssistantStatus  string   `json:"assistant_status"`
}
