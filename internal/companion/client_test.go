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
				`{"content":"Resposta acolhedora","provider":"test","model":"test-model","prompt_version":"v1","blocked":false,"language":"pt-BR","route":"normal","graph_version":"conversation-graph-v1"}`,
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

func TestHTTPClientRespondStream(t *testing.T) {
	client, err := NewHTTPClient("https://companion.example.com", "test-secret", time.Second)
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v", err)
	}
	client.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/companion/respond/stream" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Accept") != "application/x-ndjson" {
			t.Fatalf("accept = %q", r.Header.Get("Accept"))
		}
		body := strings.Join([]string{
			`{"type":"start"}`,
			`{"type":"delta","delta":"Resposta "}`,
			`{"type":"heartbeat"}`,
			`{"type":"delta","delta":"acolhedora"}`,
			`{"type":"done","response":{"content":"Resposta acolhedora","provider":"test","model":"test-model","prompt_version":"v1","blocked":false,"language":"pt-BR","route":"normal","graph_version":"conversation-graph-v1"}}`,
		}, "\n") + "\n"
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/x-ndjson"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})

	var deltas strings.Builder
	response, err := client.RespondStream(
		context.Background(),
		Request{RequestID: "request-1", Message: "Olá"},
		func(delta string) error {
			deltas.WriteString(delta)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("RespondStream() error = %v", err)
	}
	if deltas.String() != response.Content {
		t.Fatalf("deltas = %q, content = %q", deltas.String(), response.Content)
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
				`{"schema_version":"journey-report-v1","title":"Relatório","coverage":{"conversation_count":1,"user_message_count":1,"active_day_count":1,"completeness":"limited","note":"Cobertura limitada"},"summary":"Resumo do período","timeline":[],"items":[{"kind":"open_topic","title":"Tema","description":"Tema recorrente","evidence_strength":"explicit_once","source_message_ids":["message-1"],"limitations":[]}],"limitations":[],"provider":"test","model":"test-model","prompt_version":"journey-report-v1","graph_version":"journey-report-graph-v1"}`,
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
