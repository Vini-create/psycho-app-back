package email

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBrevoSenderSend(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("api-key") != "secret-key" {
			t.Fatalf("unexpected Brevo request")
		}
		var payload struct {
			Sender      struct{ Name, Email string }
			To          []struct{ Email string }
			Subject     string
			HTMLContent string `json:"htmlContent"`
			Tags        []string
			Headers     map[string]string
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload.Sender.Email != "no-reply@example.com" || len(payload.To) != 1 ||
			payload.To[0].Email != "person@example.com" || payload.Subject != "Subject" ||
			payload.HTMLContent != "<p>Hello</p>" || len(payload.Tags) != 1 ||
			payload.Headers["Idempotency-Key"] != "00000000-0000-4000-8000-000000000001" {
			t.Fatalf("unexpected payload: %+v", payload)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"messageId":"brevo-123"}`))
	}))
	defer server.Close()

	sender, err := NewBrevoSender("secret-key", "Sinapsa", "no-reply@example.com", time.Second)
	if err != nil {
		t.Fatalf("NewBrevoSender() error = %v", err)
	}
	sender.endpoint = server.URL
	messageID, err := sender.Send(context.Background(), Message{
		To: "person@example.com", Subject: "Subject", HTMLContent: "<p>Hello</p>", Tags: []string{"auth"},
		IdempotencyKey: "00000000-0000-4000-8000-000000000001",
	})
	if err != nil || messageID != "brevo-123" {
		t.Fatalf("Send() = %q, %v", messageID, err)
	}
}

func TestBrevoSenderReturnsSanitizedProviderError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"sensitive provider details"}`, http.StatusUnauthorized)
	}))
	defer server.Close()
	sender, err := NewBrevoSender("secret-key", "Sinapsa", "no-reply@example.com", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	sender.endpoint = server.URL
	_, err = sender.Send(context.Background(), Message{To: "person@example.com"})
	if err == nil || err.Error() != "transactional email delivery failed" || deliveryErrorCode(err) != "http_401" {
		t.Fatalf("unexpected error: %v", err)
	}
}
