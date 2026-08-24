package email

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const brevoEndpoint = "https://api.brevo.com/v3/smtp/email"

type Message struct {
	To             string
	Subject        string
	HTMLContent    string
	Tags           []string
	IdempotencyKey string
}

type Sender interface {
	Send(context.Context, Message) (string, error)
}

type DeliveryError struct {
	Code string
	err  error
}

func (e *DeliveryError) Error() string { return "transactional email delivery failed" }
func (e *DeliveryError) Unwrap() error { return e.err }

func deliveryErrorCode(err error) string {
	var deliveryErr *DeliveryError
	if errors.As(err, &deliveryErr) && deliveryErr.Code != "" {
		return deliveryErr.Code
	}
	return "provider_error"
}

type BrevoSender struct {
	apiKey      string
	fromName    string
	fromAddress string
	endpoint    string
	client      *http.Client
}

func NewBrevoSender(apiKey, fromName, fromAddress string, timeout time.Duration) (*BrevoSender, error) {
	apiKey = strings.TrimSpace(apiKey)
	fromName = strings.TrimSpace(fromName)
	fromAddress = strings.TrimSpace(fromAddress)
	if apiKey == "" || fromName == "" || fromAddress == "" {
		return nil, fmt.Errorf("Brevo sender configuration is required")
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("Brevo timeout must be greater than zero")
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 20
	transport.MaxIdleConnsPerHost = 10
	transport.IdleConnTimeout = 90 * time.Second

	return &BrevoSender{
		apiKey: apiKey, fromName: fromName, fromAddress: fromAddress,
		endpoint: brevoEndpoint,
		client:   &http.Client{Timeout: timeout, Transport: transport},
	}, nil
}

func (s *BrevoSender) Send(ctx context.Context, message Message) (string, error) {
	payload := map[string]any{
		"sender":      map[string]string{"name": s.fromName, "email": s.fromAddress},
		"to":          []map[string]string{{"email": message.To}},
		"subject":     message.Subject,
		"htmlContent": message.HTMLContent,
		"tags":        message.Tags,
	}
	if message.IdempotencyKey != "" {
		payload["headers"] = map[string]string{"Idempotency-Key": message.IdempotencyKey}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode Brevo request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create Brevo request: %w", err)
	}
	request.Header.Set("api-key", s.apiKey)
	request.Header.Set("content-type", "application/json")
	request.Header.Set("accept", "application/json")

	response, err := s.client.Do(request)
	if err != nil {
		return "", &DeliveryError{Code: "network_error", err: err}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return "", &DeliveryError{
			Code: fmt.Sprintf("http_%d", response.StatusCode),
			err:  fmt.Errorf("Brevo returned status %d", response.StatusCode),
		}
	}

	var result struct {
		MessageID string `json:"messageId"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 16*1024))
	if err := decoder.Decode(&result); err != nil || strings.TrimSpace(result.MessageID) == "" {
		return "", &DeliveryError{Code: "invalid_response", err: err}
	}
	return result.MessageID, nil
}

type MockSender struct{}

func (MockSender) Send(_ context.Context, _ Message) (string, error) {
	return "mock-message", nil
}
