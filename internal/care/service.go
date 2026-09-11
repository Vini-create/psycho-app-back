package care

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

var validProfessionTypes = map[string]struct{}{
	"psychologist": {}, "psychiatrist": {}, "psychoanalyst": {},
	"therapist": {}, "psychotherapist": {}, "occupational_therapist": {},
	"counselor": {}, "other": {},
}

var professionsRequiringRegistration = map[string]struct{}{
	"psychologist": {}, "psychiatrist": {}, "occupational_therapist": {},
}

var validSharingScopes = map[string]struct{}{
	"summaries": {}, "events": {}, "marked_topics": {},
}

type ServiceConfig struct {
	InvitationTTL        time.Duration
	ConsentPolicyVersion string
	PatientAppURL        string
}

type Service struct {
	repository *Repository
	config     ServiceConfig
	now        func() time.Time
}

func NewService(repository *Repository, config ServiceConfig) (*Service, error) {
	if repository == nil {
		return nil, fmt.Errorf("care repository is required")
	}
	if config.InvitationTTL <= 0 {
		return nil, fmt.Errorf("invitation TTL must be greater than zero")
	}
	if strings.TrimSpace(config.ConsentPolicyVersion) == "" ||
		len(config.ConsentPolicyVersion) > 64 {
		return nil, fmt.Errorf("valid consent policy version is required")
	}
	config.PatientAppURL = strings.TrimRight(strings.TrimSpace(config.PatientAppURL), "/")
	patientAppURL, err := url.Parse(config.PatientAppURL)
	if err != nil || patientAppURL.Scheme == "" || patientAppURL.Host == "" ||
		(patientAppURL.Scheme != "http" && patientAppURL.Scheme != "https") {
		return nil, fmt.Errorf("valid patient application URL is required")
	}
	return &Service{repository: repository, config: config, now: time.Now}, nil
}

func (s *Service) UpsertProfessionalProfile(
	ctx context.Context,
	professionalUserID string,
	input ProfessionalProfileInput,
) (ProfessionalProfile, error) {
	normalized, err := normalizeProfessionalProfile(input)
	if err != nil {
		return ProfessionalProfile{}, err
	}
	if err := s.repository.UpsertProfessionalProfile(
		ctx, professionalUserID, normalized, s.now().UTC(),
	); err != nil {
		return ProfessionalProfile{}, err
	}
	return s.repository.GetProfessionalProfile(ctx, professionalUserID)
}

func (s *Service) GetProfessionalProfile(
	ctx context.Context,
	professionalUserID string,
) (ProfessionalProfile, error) {
	return s.repository.GetProfessionalProfile(ctx, professionalUserID)
}

func (s *Service) CreateInvitation(
	ctx context.Context,
	professionalUserID string,
	targetEmail string,
) (Invitation, error) {
	email, err := normalizeEmail(targetEmail)
	if err != nil {
		return Invitation{}, err
	}
	rawToken, tokenHash, err := newInvitationToken()
	if err != nil {
		return Invitation{}, err
	}
	invitationURL, err := buildInvitationURL(s.config.PatientAppURL, rawToken)
	if err != nil {
		return Invitation{}, fmt.Errorf("build invitation URL: %w", err)
	}
	now := s.now().UTC()
	invitation, err := s.repository.CreateInvitation(
		ctx,
		professionalUserID,
		email,
		tokenHash,
		now.Add(s.config.InvitationTTL),
		now,
	)
	if err != nil {
		return Invitation{}, err
	}
	invitation.InvitationToken = rawToken
	invitation.InvitationURL = invitationURL
	return invitation, nil
}

func (s *Service) ListInvitations(
	ctx context.Context,
	professionalUserID string,
) ([]Invitation, error) {
	return s.repository.ListInvitations(ctx, professionalUserID, s.now().UTC())
}

func (s *Service) RevokeInvitation(
	ctx context.Context,
	professionalUserID string,
	invitationID string,
) error {
	if _, err := uuid.Parse(invitationID); err != nil {
		return ErrInvalidInput
	}
	return s.repository.RevokeInvitation(
		ctx, professionalUserID, invitationID, s.now().UTC(),
	)
}

func (s *Service) PreviewInvitation(
	ctx context.Context,
	rawToken string,
) (InvitationPreview, error) {
	if len(rawToken) < 32 || len(rawToken) > 200 {
		return InvitationPreview{}, ErrInvalidToken
	}
	return s.repository.PreviewInvitation(ctx, hashInvitationToken(rawToken), s.now().UTC())
}

func (s *Service) AcceptInvitation(
	ctx context.Context,
	appUserID string,
	rawToken string,
	consentScopes []string,
	client ClientInfo,
) (string, error) {
	if len(rawToken) < 32 || len(rawToken) > 200 {
		return "", ErrInvalidToken
	}
	scopes, err := normalizeSharingScopes(consentScopes)
	if err != nil {
		return "", err
	}
	return s.repository.AcceptInvitation(
		ctx,
		appUserID,
		hashInvitationToken(rawToken),
		scopes,
		s.config.ConsentPolicyVersion,
		client,
		s.now().UTC(),
	)
}

func (s *Service) ListAppConnections(
	ctx context.Context,
	appUserID string,
) ([]Connection, error) {
	return s.repository.ListAppConnections(ctx, appUserID)
}

func (s *Service) ReplaceConnectionConsents(
	ctx context.Context,
	appUserID string,
	connectionID string,
	scopes []string,
	client ClientInfo,
) error {
	if _, err := uuid.Parse(connectionID); err != nil {
		return ErrInvalidInput
	}
	normalized, err := normalizeSharingScopes(scopes)
	if err != nil {
		return err
	}
	return s.repository.ReplaceConnectionConsents(
		ctx,
		appUserID,
		connectionID,
		normalized,
		s.config.ConsentPolicyVersion,
		client,
		s.now().UTC(),
	)
}

func (s *Service) EndAppConnection(
	ctx context.Context,
	appUserID string,
	connectionID string,
) error {
	if _, err := uuid.Parse(connectionID); err != nil {
		return ErrInvalidInput
	}
	return s.repository.EndConnectionByApp(ctx, appUserID, connectionID, s.now().UTC())
}

func (s *Service) ListProfessionalPatients(
	ctx context.Context,
	professionalUserID string,
) ([]Connection, error) {
	return s.repository.ListProfessionalPatients(ctx, professionalUserID)
}

func (s *Service) GetProfessionalPatient(
	ctx context.Context,
	professionalUserID string,
	connectionID string,
) (Connection, error) {
	if _, err := uuid.Parse(connectionID); err != nil {
		return Connection{}, ErrInvalidInput
	}
	return s.repository.GetProfessionalPatient(ctx, professionalUserID, connectionID)
}

func (s *Service) EndProfessionalConnection(
	ctx context.Context,
	professionalUserID string,
	connectionID string,
) error {
	if _, err := uuid.Parse(connectionID); err != nil {
		return ErrInvalidInput
	}
	return s.repository.EndConnectionByProfessional(
		ctx, professionalUserID, connectionID, s.now().UTC(),
	)
}

func normalizeProfessionalProfile(input ProfessionalProfileInput) (ProfessionalProfileInput, error) {
	input.ProfessionType = strings.TrimSpace(input.ProfessionType)
	if _, exists := validProfessionTypes[input.ProfessionType]; !exists {
		return ProfessionalProfileInput{}, ErrInvalidInput
	}
	input.RegistrationCountryCode = strings.ToUpper(strings.TrimSpace(input.RegistrationCountryCode))
	input.RegistrationRegion = strings.TrimSpace(input.RegistrationRegion)
	input.RegistrationNumber = strings.TrimSpace(input.RegistrationNumber)
	input.Bio = strings.TrimSpace(input.Bio)
	if len(input.RegistrationCountryCode) != 2 || input.RegistrationRegion == "" {
		return ProfessionalProfileInput{}, ErrInvalidInput
	}
	if _, required := professionsRequiringRegistration[input.ProfessionType]; required &&
		input.RegistrationNumber == "" {
		return ProfessionalProfileInput{}, ErrInvalidInput
	}
	if utf8.RuneCountInString(input.RegistrationRegion) > 100 ||
		utf8.RuneCountInString(input.RegistrationNumber) > 100 ||
		utf8.RuneCountInString(input.Bio) > 2000 || len(input.Certifications) > 50 {
		return ProfessionalProfileInput{}, ErrInvalidInput
	}
	certifications := make([]string, 0, len(input.Certifications))
	for _, certification := range input.Certifications {
		certification = strings.Join(strings.Fields(certification), " ")
		if certification == "" || utf8.RuneCountInString(certification) > 200 {
			return ProfessionalProfileInput{}, ErrInvalidInput
		}
		certifications = append(certifications, certification)
	}
	input.Certifications = certifications
	return input, nil
}

func normalizeSharingScopes(values []string) ([]string, error) {
	if len(values) == 0 || len(values) > len(validSharingScopes) {
		return nil, ErrInvalidInput
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if _, valid := validSharingScopes[value]; !valid {
			return nil, ErrInvalidInput
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func normalizeEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) < 3 || len(value) > 254 || !utf8.ValidString(value) {
		return "", ErrInvalidInput
	}
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value {
		return "", ErrInvalidInput
	}
	return value, nil
}

func newInvitationToken() (string, string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", "", fmt.Errorf("generate invitation token: %w", err)
	}
	raw := base64.RawURLEncoding.EncodeToString(random)
	return raw, hashInvitationToken(raw), nil
}

func hashInvitationToken(raw string) string {
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:])
}

func buildInvitationURL(patientAppURL, rawToken string) (string, error) {
	return url.JoinPath(patientAppURL, "convite", rawToken)
}

func maskEmail(email string) string {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 || len(parts[0]) == 0 {
		return "***"
	}
	return string([]rune(parts[0])[0]) + "***@" + parts[1]
}
