package chat

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestNormalizeConsentTypes(t *testing.T) {
	got, err := normalizeConsentTypes([]string{ConsentTerms, ConsentPrivacy, ConsentTerms})
	if err != nil {
		t.Fatalf("normalizeConsentTypes() error = %v", err)
	}
	if len(got) != 2 || got[0] != ConsentTerms || got[1] != ConsentPrivacy {
		t.Fatalf("normalizeConsentTypes() = %#v", got)
	}

	if _, err := normalizeConsentTypes([]string{"unknown"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid consent error = %v", err)
	}
}

func TestValidateMessageInput(t *testing.T) {
	conversationID := uuid.NewString()
	if err := validateMessageInput(conversationID, "mensagem válida", "request-123"); err != nil {
		t.Fatalf("validateMessageInput() error = %v", err)
	}

	tests := []struct {
		name           string
		conversationID string
		content        string
		idempotencyKey string
	}{
		{name: "invalid conversation", conversationID: "invalid", content: "ok", idempotencyKey: "request-123"},
		{name: "empty content", conversationID: conversationID, content: " ", idempotencyKey: "request-123"},
		{name: "short key", conversationID: conversationID, content: "ok", idempotencyKey: "short"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateMessageInput(
				test.conversationID, test.content, test.idempotencyKey,
			); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("validateMessageInput() error = %v", err)
			}
		})
	}
}
