package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealth(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	recorder := httptest.NewRecorder()
	router := NewRouter()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Errorf(
			"unexpected status code: got %d, want %d",
			recorder.Code,
			http.StatusOK,
		)
	}

	contentType := recorder.Header().Get("Content-Type")

	if contentType != "application/json" {
		t.Errorf(
			"unexpected content type: got %q, want %q",
			contentType,
			"application/json",
		)
	}

	body := recorder.Body.String()
	expectedBody := `{"status":"ok"}`

	if body != expectedBody {
		t.Errorf(
			"unexpected body: got %q, want %q",
			body,
			expectedBody,
		)
	}
}
