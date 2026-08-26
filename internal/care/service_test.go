package care

import (
	"errors"
	"testing"
	"time"
)

func TestNewServiceRequiresPatientApplicationURL(t *testing.T) {
	config := ServiceConfig{
		InvitationTTL:        7 * 24 * time.Hour,
		ConsentPolicyVersion: "test-v1",
	}
	if _, err := NewService(&Repository{}, config); err == nil {
		t.Fatal("NewService() error = nil, want invalid patient application URL")
	}

	config.PatientAppURL = "https://app.example.com/"
	service, err := NewService(&Repository{}, config)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if service.config.PatientAppURL != "https://app.example.com" {
		t.Fatalf("PatientAppURL = %q, want normalized origin", service.config.PatientAppURL)
	}
}

func TestBuildInvitationURL(t *testing.T) {
	got, err := buildInvitationURL(
		"https://app.example.com",
		"private-invitation-token",
	)
	if err != nil {
		t.Fatalf("buildInvitationURL() error = %v", err)
	}
	const want = "https://app.example.com/convite/private-invitation-token"
	if got != want {
		t.Fatalf("buildInvitationURL() = %q, want %q", got, want)
	}
}

func TestNormalizeProfessionalProfileRequiresRegistration(t *testing.T) {
	valid := ProfessionalProfileInput{
		ProfessionType: "psychologist", RegistrationCountryCode: "BR",
		RegistrationRegion: "SP", RegistrationNumber: "06/123456",
	}
	tests := []struct {
		name   string
		mutate func(*ProfessionalProfileInput)
	}{
		{name: "country", mutate: func(input *ProfessionalProfileInput) { input.RegistrationCountryCode = "" }},
		{name: "region", mutate: func(input *ProfessionalProfileInput) { input.RegistrationRegion = " " }},
		{name: "number", mutate: func(input *ProfessionalProfileInput) { input.RegistrationNumber = "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := valid
			test.mutate(&input)
			if _, err := normalizeProfessionalProfile(input); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("normalizeProfessionalProfile() error = %v, want ErrInvalidInput", err)
			}
		})
	}
	if _, err := normalizeProfessionalProfile(valid); err != nil {
		t.Fatalf("normalizeProfessionalProfile(valid) error = %v", err)
	}
}
