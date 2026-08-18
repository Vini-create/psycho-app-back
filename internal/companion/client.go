package companion

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrUnavailable = errors.New("companion service is unavailable")

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Request struct {
	RequestID      string    `json:"request_id"`
	ConversationID string    `json:"conversation_id"`
	UserID         string    `json:"user_id"`
	Message        string    `json:"message"`
	History        []Message `json:"history"`
}

type Response struct {
	Content       string `json:"content"`
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	PromptVersion string `json:"prompt_version"`
	Blocked       bool   `json:"blocked"`
	BlockReason   string `json:"block_reason,omitempty"`
}

type Client interface {
	Respond(ctx context.Context, request Request) (Response, error)
	ProcessContext(ctx context.Context, request ContextRequest) (ContextResponse, error)
}

type UnavailableClient struct{}

func (UnavailableClient) Respond(context.Context, Request) (Response, error) {
	return Response{}, ErrUnavailable
}

func (UnavailableClient) ProcessContext(context.Context, ContextRequest) (ContextResponse, error) {
	return ContextResponse{}, ErrUnavailable
}

type ContextMessage struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type ContextRequest struct {
	RequestID    string           `json:"request_id"`
	ConnectionID string           `json:"connection_id"`
	UserID       string           `json:"user_id"`
	PeriodStart  time.Time        `json:"period_start"`
	PeriodEnd    time.Time        `json:"period_end"`
	Messages     []ContextMessage `json:"messages"`
}

type ContextItem struct {
	Kind             string     `json:"kind"`
	Description      string     `json:"description"`
	Confidence       *float64   `json:"confidence,omitempty"`
	OccurredAt       *time.Time `json:"occurred_at,omitempty"`
	SourceMessageIDs []string   `json:"source_message_ids"`
}

type ContextResponse struct {
	Summary       string        `json:"summary"`
	Items         []ContextItem `json:"items"`
	Provider      string        `json:"provider"`
	Model         string        `json:"model"`
	PromptVersion string        `json:"prompt_version"`
}

type HTTPClient struct {
	endpoint string
	apiKey   string
	client   *http.Client
}

func NewHTTPClient(baseURL, apiKey string, timeout time.Duration) (*HTTPClient, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("companion base URL is invalid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("companion base URL must use HTTP or HTTPS")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("companion API key is required")
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("companion timeout must be greater than zero")
	}

	return &HTTPClient{
		endpoint: baseURL + "/v1/companion/respond",
		apiKey:   apiKey,
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (c *HTTPClient) Respond(ctx context.Context, input Request) (Response, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return Response{}, fmt.Errorf("encode companion request: %w", err)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.endpoint,
		bytes.NewReader(body),
	)
	if err != nil {
		return Response{}, fmt.Errorf("create companion request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Request-ID", input.RequestID)

	response, err := c.client.Do(request)
	if err != nil {
		return Response{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return Response{}, fmt.Errorf("%w: status %d", ErrUnavailable, response.StatusCode)
	}

	limitedBody := io.LimitReader(response.Body, 64*1024)
	decoder := json.NewDecoder(limitedBody)
	decoder.DisallowUnknownFields()

	var output Response
	if err := decoder.Decode(&output); err != nil {
		return Response{}, fmt.Errorf("%w: decode response", ErrUnavailable)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Response{}, fmt.Errorf("%w: response must contain one JSON object", ErrUnavailable)
	}
	if strings.TrimSpace(output.Content) == "" {
		return Response{}, fmt.Errorf("%w: empty response", ErrUnavailable)
	}

	return output, nil
}

func (c *HTTPClient) ProcessContext(
	ctx context.Context,
	input ContextRequest,
) (ContextResponse, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return ContextResponse{}, fmt.Errorf("encode context request: %w", err)
	}
	endpoint := strings.TrimSuffix(c.endpoint, "/v1/companion/respond") + "/v1/context/process"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return ContextResponse{}, fmt.Errorf("create context request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Request-ID", input.RequestID)

	response, err := c.client.Do(request)
	if err != nil {
		return ContextResponse{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return ContextResponse{}, fmt.Errorf("%w: status %d", ErrUnavailable, response.StatusCode)
	}

	decoder := json.NewDecoder(io.LimitReader(response.Body, 256*1024))
	decoder.DisallowUnknownFields()
	var output ContextResponse
	if err := decoder.Decode(&output); err != nil {
		return ContextResponse{}, fmt.Errorf("%w: decode context response", ErrUnavailable)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ContextResponse{}, fmt.Errorf("%w: context response must contain one JSON object", ErrUnavailable)
	}
	return output, nil
}
