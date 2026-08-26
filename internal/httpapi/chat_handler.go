package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Vini-create/psycho-app-back/internal/auth"
	"github.com/Vini-create/psycho-app-back/internal/chat"
)

type ChatHandler struct {
	service        *chat.Service
	messageLimiter *fixedWindowLimiter
}

func NewChatHandler(service *chat.Service) *ChatHandler {
	return &ChatHandler{
		service: service, messageLimiter: newFixedWindowLimiter(20, time.Minute),
	}
}

func (h *ChatHandler) RegisterRoutes(mux *http.ServeMux, authHandler *AuthHandler) {
	requireApp := func(handler http.HandlerFunc) http.HandlerFunc {
		return authHandler.requireAuth(auth.AudienceApp, handler)
	}

	mux.HandleFunc("GET /v1/app/consents", requireApp(h.listConsents))
	mux.HandleFunc("POST /v1/app/consents", requireApp(h.grantConsents))
	mux.HandleFunc("DELETE /v1/app/consents/{consentType}", requireApp(h.revokeConsent))
	mux.HandleFunc("GET /v1/app/conversations", requireApp(h.listConversations))
	mux.HandleFunc("POST /v1/app/conversations", requireApp(h.createConversation))
	mux.HandleFunc("PATCH /v1/app/conversations/{conversationID}", requireApp(h.renameConversation))
	mux.HandleFunc("DELETE /v1/app/conversations/{conversationID}", requireApp(h.archiveConversation))
	mux.HandleFunc("GET /v1/app/conversations/{conversationID}/messages", requireApp(h.listMessages))
	mux.HandleFunc("POST /v1/app/conversations/{conversationID}/messages", requireApp(h.sendMessage))
	mux.HandleFunc("POST /v1/app/messages/{messageID}/retry", requireApp(h.retryMessage))
}

func (h *ChatHandler) listConsents(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	consents, err := h.service.ListConsents(r.Context(), principal.AccountID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"consents": consents})
}

func (h *ChatHandler) grantConsents(w http.ResponseWriter, r *http.Request) {
	type request struct {
		ConsentTypes []string `json:"consent_types"`
	}
	var body request
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
		return
	}
	principal := principalFromContext(r.Context())
	requestClient := clientInfo(r)
	consents, err := h.service.GrantConsents(
		r.Context(),
		principal.AccountID,
		body.ConsentTypes,
		chat.ClientInfo{IPAddress: requestClient.IPAddress, UserAgent: requestClient.UserAgent},
	)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"consents": consents})
}

func (h *ChatHandler) revokeConsent(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	if err := h.service.RevokeConsent(
		r.Context(), principal.AccountID, r.PathValue("consentType"),
	); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ChatHandler) createConversation(w http.ResponseWriter, r *http.Request) {
	type request struct {
		Title string `json:"title"`
	}
	var body request
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
		return
	}
	principal := principalFromContext(r.Context())
	conversation, err := h.service.CreateConversation(
		r.Context(), principal.AccountID, body.Title,
	)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, conversation)
}

func (h *ChatHandler) listConversations(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	conversations, err := h.service.ListConversations(r.Context(), principal.AccountID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversations": conversations})
}

func (h *ChatHandler) renameConversation(w http.ResponseWriter, r *http.Request) {
	type request struct {
		Title string `json:"title"`
	}
	var body request
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
		return
	}
	principal := principalFromContext(r.Context())
	conversation, err := h.service.RenameConversation(
		r.Context(), principal.AccountID, r.PathValue("conversationID"), body.Title,
	)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, conversation)
}

func (h *ChatHandler) archiveConversation(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	if err := h.service.ArchiveConversation(
		r.Context(), principal.AccountID, r.PathValue("conversationID"),
	); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ChatHandler) listMessages(w http.ResponseWriter, r *http.Request) {
	beforeSequence, err := parseOptionalInt64(r.URL.Query().Get("before_sequence"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", "before_sequence must be a positive integer")
		return
	}
	limit, err := parseOptionalInt(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", "limit must be a positive integer")
		return
	}

	principal := principalFromContext(r.Context())
	messages, err := h.service.ListMessages(
		r.Context(),
		principal.AccountID,
		r.PathValue("conversationID"),
		beforeSequence,
		limit,
	)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": messages})
}

func (h *ChatHandler) sendMessage(w http.ResponseWriter, r *http.Request) {
	type request struct {
		Content string `json:"content"`
	}
	var body request
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
		return
	}
	principal := principalFromContext(r.Context())
	if !h.messageLimiter.Allow("message:" + principal.AccountID) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many message requests")
		return
	}
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		h.streamMessage(w, r, principal.AccountID, body.Content)
		return
	}
	result, err := h.service.SendMessage(
		r.Context(),
		principal.AccountID,
		r.PathValue("conversationID"),
		body.Content,
		r.Header.Get("Idempotency-Key"),
	)
	if err != nil {
		h.handleError(w, err)
		return
	}
	status := http.StatusCreated
	if result.AssistantStatus == "pending" || result.AssistantStatus == "failed" {
		status = http.StatusAccepted
	}
	writeJSON(w, status, result)
}

type sseWriter struct {
	mu      sync.Mutex
	w       http.ResponseWriter
	flusher http.Flusher
}

func (writer *sseWriter) event(name string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if _, err := writer.w.Write([]byte("event: " + name + "\ndata: ")); err != nil {
		return err
	}
	if _, err := writer.w.Write(data); err != nil {
		return err
	}
	if _, err := writer.w.Write([]byte("\n\n")); err != nil {
		return err
	}
	writer.flusher.Flush()
	return nil
}

func (writer *sseWriter) heartbeat() error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if _, err := writer.w.Write([]byte(": keep-alive\n\n")); err != nil {
		return err
	}
	writer.flusher.Flush()
	return nil
}

func (h *ChatHandler) streamMessage(
	w http.ResponseWriter,
	r *http.Request,
	appUserID string,
	content string,
) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming_unavailable", "streaming is unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Vary", "Accept")
	w.WriteHeader(http.StatusOK)
	stream := &sseWriter{w: w, flusher: flusher}
	if err := stream.event("assistant.started", map[string]any{}); err != nil {
		return
	}

	stopHeartbeat := make(chan struct{})
	heartbeatDone := make(chan struct{})
	defer func() {
		close(stopHeartbeat)
		<-heartbeatDone
	}()
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(12 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := stream.heartbeat(); err != nil {
					return
				}
			case <-stopHeartbeat:
				return
			case <-r.Context().Done():
				return
			}
		}
	}()

	result, err := h.service.SendMessageStream(
		r.Context(),
		appUserID,
		r.PathValue("conversationID"),
		content,
		r.Header.Get("Idempotency-Key"),
		func(delta string) error {
			return stream.event("assistant.delta", map[string]string{"delta": delta})
		},
	)
	if err != nil {
		slog.Error("chat stream failed", "error", err)
		_ = stream.event("assistant.error", map[string]string{
			"code": "generation_failed", "message": "Não consegui concluir a resposta agora.",
		})
		return
	}
	_ = stream.event("assistant.completed", result)
}

func (h *ChatHandler) retryMessage(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	if !h.messageLimiter.Allow("message:" + principal.AccountID) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many message requests")
		return
	}
	result, err := h.service.RetryMessage(
		r.Context(), principal.AccountID, r.PathValue("messageID"),
	)
	if err != nil {
		h.handleError(w, err)
		return
	}
	status := http.StatusOK
	if result.AssistantStatus == "pending" || result.AssistantStatus == "failed" {
		status = http.StatusAccepted
	}
	writeJSON(w, status, result)
}

func (h *ChatHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, chat.ErrInvalidInput):
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", "request values are invalid")
	case errors.Is(err, chat.ErrConsentRequired):
		writeError(w, http.StatusForbidden, "consent_required", "current terms, privacy and AI processing consents are required")
	case errors.Is(err, chat.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "operation is not allowed")
	case errors.Is(err, chat.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "resource was not found")
	case errors.Is(err, chat.ErrConflict):
		writeError(w, http.StatusConflict, "state_conflict", "resource cannot be changed in its current state")
	default:
		slog.Error("chat request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
	}
}

func parseOptionalInt64(value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, errors.New("invalid positive integer")
	}
	return parsed, nil
}

func parseOptionalInt(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, errors.New("invalid positive integer")
	}
	return parsed, nil
}
