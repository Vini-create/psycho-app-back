package auth

import (
	"errors"
	"testing"
)

func TestPasswordHasher(t *testing.T) {
	hasher := PasswordHasher{}
	password := "correct-horse-battery-staple"

	encoded, err := hasher.Hash(password)
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}

	matches, err := hasher.Compare(password, encoded)
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}
	if !matches {
		t.Fatal("Compare() = false, want true")
	}

	matches, err = hasher.Compare("incorrect-password", encoded)
	if err != nil {
		t.Fatalf("Compare() incorrect password error = %v", err)
	}
	if matches {
		t.Fatal("Compare() incorrect password = true, want false")
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword("short"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("ValidatePassword() error = %v, want ErrWeakPassword", err)
	}

	if err := ValidatePassword("a-secure-passphrase"); err != nil {
		t.Fatalf("ValidatePassword() unexpected error = %v", err)
	}
}
