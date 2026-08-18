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
	ID          string     `json:"id"`
	Kind        string     `json:"kind"`
	Description string     `json:"description"`
	Confidence  *float64   `json:"confidence,omitempty"`
	OccurredAt  *time.Time `json:"occurred_at,omitempty"`
}

type Summary struct {
	ID            string    `json:"id"`
	ConnectionID  string    `json:"connection_id"`
	PeriodStart   time.Time `json:"period_start"`
	PeriodEnd     time.Time `json:"period_end"`
	Summary       string    `json:"summary"`
	Items         []Item    `json:"items"`
	Provider      string    `json:"provider"`
	Model         string    `json:"model"`
	PromptVersion string    `json:"prompt_version"`
	CreatedAt     time.Time `json:"created_at"`
}

type GenerationResult struct {
	JobID   string   `json:"job_id"`
	Status  string   `json:"status"`
	Summary *Summary `json:"context,omitempty"`
}
