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
		"GET /v1/professional/patients/{connectionID}/contexts",
		requireProfessional(h.list),
	)
	mux.HandleFunc(
		"POST /v1/professional/patients/{connectionID}/context-report-requests",
		requireProfessional(h.createRequest),
	)
	mux.HandleFunc(
		"GET /v1/professional/patients/{connectionID}/context-report-requests",
		requireProfessional(h.listRequestsForProfessional),
	)
	mux.HandleFunc(
		"GET /v1/app/connections/{connectionID}/context-report-requests",
		requireApp(h.listRequestsForApp),
	)
	mux.HandleFunc(
		"POST /v1/app/context-report-requests/{requestID}/send",
		requireApp(h.sendRequestedReport),
	)
}

func (h *InsightHandler) createRequest(w http.ResponseWriter, r *http.Request) {
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
	result, err := h.service.CreateReportRequest(
		r.Context(), principal.AccountID, r.PathValue("connectionID"),
		body.PeriodStart, body.PeriodEnd,
	)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (h *InsightHandler) listRequestsForProfessional(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	requests, err := h.service.ListReportRequestsForProfessional(
		r.Context(), principal.AccountID, r.PathValue("connectionID"),
	)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": requests})
}

func (h *InsightHandler) listRequestsForApp(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	requests, err := h.service.ListReportRequestsForApp(
		r.Context(), principal.AccountID, r.PathValue("connectionID"),
	)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": requests})
}

func (h *InsightHandler) sendRequestedReport(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	result, err := h.service.SendRequestedReport(
		r.Context(), principal.AccountID, r.PathValue("requestID"),
	)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, result)
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

func (h *InsightHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, insight.ErrInvalidInput):
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", "period or connection is invalid")
	case errors.Is(err, insight.ErrForbidden):
		writeError(w, http.StatusForbidden, "context_consent_required", "current consent does not allow this context")
	case errors.Is(err, insight.ErrConflict):
		writeError(w, http.StatusConflict, "context_request_conflict", "this period already has an open request")
	case errors.Is(err, insight.ErrRequestResolved):
		writeError(w, http.StatusConflict, "context_request_resolved", "this report request was already answered")
	case errors.Is(err, insight.ErrSubscriptionRequired):
		writeError(w, http.StatusPaymentRequired, "subscription_required", "an active professional subscription is required")
	case errors.Is(err, insight.ErrProfileIncomplete):
		writeError(w, http.StatusConflict, "profile_incomplete", "professional profile must be completed first")
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
