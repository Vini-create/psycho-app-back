package insight

import (
	"errors"
	"time"
)

var (
	ErrNotFound             = errors.New("resource not found")
	ErrInvalidInput         = errors.New("invalid input")
	ErrForbidden            = errors.New("operation is forbidden")
	ErrConflict             = errors.New("processing already in progress")
	ErrRequestResolved      = errors.New("report request is already resolved")
	ErrSubscriptionRequired = errors.New("active subscription is required")
	ErrNoMessages           = errors.New("period has no messages")
	ErrPeriodTooLarge       = errors.New("period contains too many messages")
)

type Item struct {
	ID               string     `json:"id"`
	Kind             string     `json:"kind"`
	Title            string     `json:"title"`
	Description      string     `json:"description"`
	Impact           string     `json:"impact,omitempty"`
	EvidenceStrength string     `json:"evidence_strength"`
	EmotionalValence string     `json:"emotional_valence,omitempty"`
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

type ReportRequest struct {
	ID                      string     `json:"id"`
	ConnectionID            string     `json:"connection_id"`
	ProfessionalDisplayName string     `json:"professional_display_name,omitempty"`
	PatientDisplayName      string     `json:"patient_display_name,omitempty"`
	PeriodStart             time.Time  `json:"period_start"`
	PeriodEnd               time.Time  `json:"period_end"`
	Status                  string     `json:"status"`
	RequestedAt             time.Time  `json:"requested_at"`
	SentAt                  *time.Time `json:"sent_at"`
}

type SendReportResult struct {
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
}
