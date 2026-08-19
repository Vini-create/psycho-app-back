package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/Vini-create/psycho-app-back/internal/auth"
	"github.com/Vini-create/psycho-app-back/internal/insight"
)

type InsightHandler struct {
	service *insight.Service
}

func NewInsightHandler(service *insight.Service) *InsightHandler {
	return &InsightHandler{service: service}
}

func (h *InsightHandler) RegisterRoutes(mux *http.ServeMux, authHandler *AuthHandler) {
	requireApp := func(handler http.HandlerFunc) http.HandlerFunc {
		return authHandler.requireAuth(auth.AudienceApp, handler)
	}
	requireProfessional := func(handler http.HandlerFunc) http.HandlerFunc {
		return authHandler.requireAuth(auth.AudienceProfessional, requireMFA(handler))
	}

	mux.HandleFunc(
		"POST /v1/professional/patients/{connectionID}/contexts",
		requireProfessional(h.generate),
	)
	mux.HandleFunc(
		"GET /v1/professional/patients/{connectionID}/contexts",
		requireProfessional(h.list),
	)
	mux.HandleFunc(
		"GET /v1/professional/context-jobs/{jobID}",
		requireProfessional(h.getJob),
	)
	mux.HandleFunc("GET /v1/app/context-reports", requireApp(h.listForApp))
	mux.HandleFunc(
		"POST /v1/app/context-reports/{reportID}/review",
		requireApp(h.review),
	)
}

func (h *InsightHandler) generate(w http.ResponseWriter, r *http.Request) {
	type request struct {
		PeriodStart time.Time `json:"period_start"`
		PeriodEnd   time.Time `json:"period_end"`
	}

	var body request
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
		return
	}

	principal := principalFromContext(r.Context())
	result, err := h.service.Generate(
		r.Context(), principal.AccountID, r.PathValue("connectionID"),
		body.PeriodStart, body.PeriodEnd,
	)
	if err != nil {
		h.handleError(w, err)
		return
	}

	writeJSON(w, http.StatusAccepted, result)
}

func (h *InsightHandler) getJob(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	job, err := h.service.GetJob(r.Context(), principal.AccountID, r.PathValue("jobID"))
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (h *InsightHandler) list(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	contexts, err := h.service.List(
		r.Context(), principal.AccountID, r.PathValue("connectionID"),
	)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"contexts": contexts})
}

func (h *InsightHandler) listForApp(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	contexts, err := h.service.ListForApp(r.Context(), principal.AccountID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"contexts": contexts})
}

func (h *InsightHandler) review(w http.ResponseWriter, r *http.Request) {
	type request struct {
		Decision                 string   `json:"decision"`
		ExcludedItemIDs          []string `json:"excluded_item_ids"`
		ExcludedTimelineEntryIDs []string `json:"excluded_timeline_entry_ids"`
	}
	var body request
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
		return
	}
	principal := principalFromContext(r.Context())
	if err := h.service.Review(
		r.Context(), principal.AccountID, r.PathValue("reportID"),
		body.Decision, body.ExcludedItemIDs, body.ExcludedTimelineEntryIDs,
	); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *InsightHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, insight.ErrInvalidInput):
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", "period or connection is invalid")
	case errors.Is(err, insight.ErrForbidden):
		writeError(w, http.StatusForbidden, "context_consent_required", "current consent does not allow this context")
	case errors.Is(err, insight.ErrConflict):
		writeError(w, http.StatusConflict, "context_processing", "this period is already being processed")
	case errors.Is(err, insight.ErrNoMessages):
		writeError(w, http.StatusUnprocessableEntity, "context_no_messages", "the selected period has no eligible messages")
	case errors.Is(err, insight.ErrPeriodTooLarge):
		writeError(w, http.StatusUnprocessableEntity, "context_period_too_large", "the selected period contains too many messages")
	case errors.Is(err, insight.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "active connection was not found")
	default:
		slog.Error("context request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
	}
}
