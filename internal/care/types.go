package care

import (
	"errors"
	"time"
)

var (
	ErrNotFound     = errors.New("resource not found")
	ErrInvalidInput = errors.New("invalid input")
	ErrConflict     = errors.New("resource conflict")
	ErrForbidden    = errors.New("operation is forbidden")
	ErrInvalidToken = errors.New("invalid or expired invitation")
)

type ClientInfo struct {
	IPAddress string
	UserAgent string
}

type ProfessionalProfileInput struct {
	ProfessionType          string
	RegistrationCountryCode string
	RegistrationRegion      string
	RegistrationNumber      string
	Bio                     string
	Certifications          []string
}

type ProfessionalProfile struct {
	ProfessionalUserID      string   `json:"professional_user_id"`
	DisplayName             string   `json:"display_name"`
	Email                   string   `json:"email"`
	ProfessionType          string   `json:"profession_type"`
	RegistrationCountryCode *string  `json:"registration_country_code,omitempty"`
	RegistrationRegion      *string  `json:"registration_region,omitempty"`
	RegistrationNumber      *string  `json:"registration_number,omitempty"`
	Bio                     *string  `json:"bio,omitempty"`
	Certifications          []string `json:"certifications"`
	VerificationStatus      string   `json:"verification_status"`
	OrganizationID          string   `json:"organization_id"`
	OrganizationName        string   `json:"organization_name"`
	MembershipID            string   `json:"membership_id"`
	Plan                    string   `json:"plan"`
	OnboardingComplete      bool     `json:"onboarding_complete"`
}

type Invitation struct {
	ID              string     `json:"id"`
	TargetEmail     string     `json:"target_email"`
	Status          string     `json:"status"`
	ExpiresAt       time.Time  `json:"expires_at"`
	AcceptedAt      *time.Time `json:"accepted_at,omitempty"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	InvitationToken string     `json:"invitation_token,omitempty"`
	InvitationURL   string     `json:"invitation_url,omitempty"`
}

type InvitationPreview struct {
	ProfessionalDisplayName string    `json:"professional_display_name"`
	ProfessionType          string    `json:"profession_type"`
	OrganizationName        string    `json:"organization_name"`
	TargetEmailHint         string    `json:"target_email_hint"`
	ExpiresAt               time.Time `json:"expires_at"`
}

type Connection struct {
	ID                      string     `json:"id"`
	Status                  string     `json:"status"`
	OrganizationID          string     `json:"organization_id"`
	OrganizationName        string     `json:"organization_name"`
	ProfessionalUserID      string     `json:"professional_user_id"`
	ProfessionalDisplayName string     `json:"professional_display_name"`
	ProfessionType          string     `json:"profession_type"`
	AppUserID               string     `json:"app_user_id"`
	PatientDisplayName      string     `json:"patient_display_name"`
	PatientEmail            string     `json:"patient_email"`
	ConsentScopes           []string   `json:"consent_scopes"`
	ActivatedAt             *time.Time `json:"activated_at,omitempty"`
	EndedAt                 *time.Time `json:"ended_at,omitempty"`
	CreatedAt               time.Time  `json:"created_at"`
}
