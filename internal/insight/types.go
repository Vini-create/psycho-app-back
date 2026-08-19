package insight

import (
	"errors"
	"time"
)

var (
	ErrNotFound       = errors.New("resource not found")
	ErrInvalidInput   = errors.New("invalid input")
	ErrForbidden      = errors.New("operation is forbidden")
	ErrConflict       = errors.New("processing already in progress")
	ErrNoMessages     = errors.New("period has no messages")
	ErrPeriodTooLarge = errors.New("period contains too many messages")
)

type Item struct {
	ID               string     `json:"id"`
	Kind             string     `json:"kind"`
	Title            string     `json:"title"`
	Description      string     `json:"description"`
	Impact           string     `json:"impact,omitempty"`
	EvidenceStrength string     `json:"evidence_strength"`
	OccurredAt       *time.Time `json:"occurred_at,omitempty"`
	Limitations      []string   `json:"limitations"`
	Included         bool       `json:"included"`
}

type Coverage struct {
	ConversationCount int    `json:"conversation_count"`
	UserMessageCount  int    `json:"user_message_count"`
	ActiveDayCount    int    `json:"active_day_count"`
	Completeness      string `json:"completeness"`
	Note              string `json:"note"`
}

type TimelineEntry struct {
	ID          string     `json:"id"`
	Description string     `json:"description"`
	OccurredAt  *time.Time `json:"occurred_at,omitempty"`
}

type Summary struct {
	ID            string          `json:"id"`
	ConnectionID  string          `json:"connection_id"`
	SchemaVersion string          `json:"schema_version"`
	Title         string          `json:"title"`
	PeriodStart   time.Time       `json:"period_start"`
	PeriodEnd     time.Time       `json:"period_end"`
	Coverage      Coverage        `json:"coverage"`
	Summary       string          `json:"summary"`
	Timeline      []TimelineEntry `json:"timeline"`
	Items         []Item          `json:"items"`
	Limitations   []string        `json:"limitations"`
	Provider      string          `json:"provider"`
	Model         string          `json:"model"`
	PromptVersion string          `json:"prompt_version"`
	GraphVersion  string          `json:"graph_version"`
	ReviewStatus  string          `json:"review_status"`
	ReviewedAt    *time.Time      `json:"reviewed_at,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
}

type GenerationResult struct {
	JobID   string   `json:"job_id"`
	Status  string   `json:"status"`
	Summary *Summary `json:"context,omitempty"`
}

type Job struct {
	ID           string     `json:"id"`
	ConnectionID string     `json:"connection_id"`
	PeriodStart  time.Time  `json:"period_start"`
	PeriodEnd    time.Time  `json:"period_end"`
	Status       string     `json:"status"`
	AttemptCount int        `json:"attempt_count"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}
