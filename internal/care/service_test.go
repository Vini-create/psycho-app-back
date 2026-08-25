package care

import (
	"errors"
	"testing"
)

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
