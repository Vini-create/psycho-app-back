package companion

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestHTTPClientRespond(t *testing.T) {
	client, err := NewHTTPClient("https://companion.example.com", "test-secret", time.Second)
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v", err)
	}
	client.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/companion/respond" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Fatalf("authorization header was not sent")
		}
		if r.Header.Get("X-Request-ID") != "request-1" {
			t.Fatalf("request ID header was not sent")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"content":"Resposta acolhedora","provider":"test","model":"test-model","prompt_version":"v1","blocked":false}`,
			)),
			Request: r,
		}, nil
	})
	response, err := client.Respond(context.Background(), Request{
		RequestID: "request-1", ConversationID: "conversation-1",
		UserID: "user-1", Message: "Olá",
	})
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if response.Content != "Resposta acolhedora" {
		t.Fatalf("content = %q", response.Content)
	}
}

func TestHTTPClientRejectsUnexpectedResponse(t *testing.T) {
	client, err := NewHTTPClient("https://companion.example.com", "test-secret", time.Second)
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v", err)
	}
	client.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("internal details")),
			Request:    r,
		}, nil
	})
	if _, err := client.Respond(context.Background(), Request{RequestID: "request-1"}); err == nil {
		t.Fatal("Respond() error = nil, want unavailable error")
	}
}

func TestHTTPClientProcessContext(t *testing.T) {
	client, err := NewHTTPClient("https://companion.example.com", "test-secret", time.Second)
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v", err)
	}
	client.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/context/process" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Fatal("authorization header was not sent")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"summary":"Resumo do período","items":[{"kind":"theme","description":"Tema recorrente","source_message_ids":["message-1"]}],"provider":"test","model":"test-model","prompt_version":"context-v1"}`,
			)),
			Request: r,
		}, nil
	})

	response, err := client.ProcessContext(context.Background(), ContextRequest{
		RequestID: "request-1", ConnectionID: "connection-1", UserID: "user-1",
		Messages: []ContextMessage{{ID: "message-1", Role: "user", Content: "Olá"}},
	})
	if err != nil {
		t.Fatalf("ProcessContext() error = %v", err)
	}
	if response.Summary != "Resumo do período" || len(response.Items) != 1 {
		t.Fatalf("ProcessContext() response = %#v", response)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
