package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Vini-create/psycho-app-back/internal/auth"
	"github.com/Vini-create/psycho-app-back/internal/checkin"
)

type CheckinHandler struct {
	service *checkin.Service
}

func NewCheckinHandler(service *checkin.Service) *CheckinHandler {
	return &CheckinHandler{service: service}
}

func (h *CheckinHandler) RegisterRoutes(mux *http.ServeMux, authHandler *AuthHandler) {
	requireApp := func(handler http.HandlerFunc) http.HandlerFunc {
		return authHandler.requireAuth(auth.AudienceApp, handler)
	}
	requireProfessional := func(handler http.HandlerFunc) http.HandlerFunc {
		return authHandler.requireAuth(auth.AudienceProfessional, requireMFA(handler))
	}

	mux.HandleFunc("POST /v1/professional/checkin-templates", requireProfessional(h.createTemplate))
	mux.HandleFunc("GET /v1/professional/checkin-templates", requireProfessional(h.listTemplates))
	mux.HandleFunc("GET /v1/professional/checkin-templates/{templateID}", requireProfessional(h.getTemplate))
	mux.HandleFunc("PUT /v1/professional/checkin-templates/{templateID}", requireProfessional(h.updateTemplate))
	mux.HandleFunc("DELETE /v1/professional/checkin-templates/{templateID}", requireProfessional(h.archiveTemplate))

	mux.HandleFunc("GET /v1/professional/patients/{connectionID}/checkin-assignments", requireProfessional(h.listAssignmentsForProfessional))
	mux.HandleFunc("POST /v1/professional/patients/{connectionID}/checkin-assignments", requireProfessional(h.createAssignment))
	mux.HandleFunc("DELETE /v1/professional/patients/{connectionID}/checkin-assignments/{assignmentID}", requireProfessional(h.revokeAssignment))

	mux.HandleFunc("GET /v1/professional/patients/{connectionID}/checkin-collection-requests", requireProfessional(h.listCollectionRequestsForProfessional))
	mux.HandleFunc("POST /v1/professional/patients/{connectionID}/checkin-collection-requests", requireProfessional(h.createCollectionRequest))
	mux.HandleFunc("GET /v1/professional/patients/{connectionID}/checkin-collections", requireProfessional(h.listCollections))

	mux.HandleFunc("GET /v1/app/checkins", requireApp(h.listCheckins))
	mux.HandleFunc("POST /v1/app/checkins/{assignmentID}/entries", requireApp(h.submitEntry))
	mux.HandleFunc("GET /v1/app/checkins/{assignmentID}/entries", requireApp(h.listEntries))
	mux.HandleFunc("GET /v1/app/connections/{connectionID}/checkin-assignments", requireApp(h.listAssignmentsForApp))
	mux.HandleFunc("POST /v1/app/checkin-assignments/{assignmentID}/accept", requireApp(h.acceptAssignment))
	mux.HandleFunc("POST /v1/app/checkin-assignments/{assignmentID}/decline", requireApp(h.declineAssignment))
	mux.HandleFunc("DELETE /v1/app/checkin-assignments/{assignmentID}", requireApp(h.endAssignment))
	mux.HandleFunc("GET /v1/app/connections/{connectionID}/checkin-collection-requests", requireApp(h.listCollectionRequestsForApp))
	mux.HandleFunc("POST /v1/app/checkin-collection-requests/{requestID}/send", requireApp(h.sendCollection))
	mux.HandleFunc("POST /v1/app/checkin-collection-requests/{requestID}/decline", requireApp(h.declineCollectionRequest))
}

/* -------------------------------------------------------- profissional */

type templateRequest struct {
	Title     string `json:"title"`
	Legend    string `json:"legend"`
	Questions []struct {
		Prompt string `json:"prompt"`
		Legend string `json:"legend"`
		// A nota não vem do cliente: é a posição do rótulo, do menor para o
		// maior. Ver internal/checkin.OptionInput.
		Options []struct {
			Label string `json:"label"`
		} `json:"options"`
	} `json:"questions"`
}

func (body templateRequest) toInput() checkin.TemplateInput {
	input := checkin.TemplateInput{
		Title:     body.Title,
		Legend:    body.Legend,
		Questions: make([]checkin.QuestionInput, 0, len(body.Questions)),
	}
	for _, question := range body.Questions {
		converted := checkin.QuestionInput{
			Prompt:  question.Prompt,
			Legend:  question.Legend,
			Options: make([]checkin.OptionInput, 0, len(question.Options)),
		}
		for _, option := range question.Options {
			converted.Options = append(converted.Options, checkin.OptionInput{
				Label: option.Label,
			})
		}
		input.Questions = append(input.Questions, converted)
	}
	return input
}

func (h *CheckinHandler) createTemplate(w http.ResponseWriter, r *http.Request) {
	var body templateRequest
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
		return
	}
	principal := principalFromContext(r.Context())
	template, err := h.service.CreateTemplate(r.Context(), principal.AccountID, body.toInput())
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, template)
}

func (h *CheckinHandler) updateTemplate(w http.ResponseWriter, r *http.Request) {
	var body templateRequest
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
		return
	}
	principal := principalFromContext(r.Context())
	template, err := h.service.UpdateTemplate(
		r.Context(), principal.AccountID, r.PathValue("templateID"), body.toInput(),
	)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, template)
}

func (h *CheckinHandler) listTemplates(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	templates, err := h.service.ListTemplates(r.Context(), principal.AccountID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": templates})
}

func (h *CheckinHandler) getTemplate(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	template, err := h.service.GetTemplate(
		r.Context(), principal.AccountID, r.PathValue("templateID"),
	)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, template)
}

func (h *CheckinHandler) archiveTemplate(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	if err := h.service.ArchiveTemplate(
		r.Context(), principal.AccountID, r.PathValue("templateID"),
	); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *CheckinHandler) createAssignment(w http.ResponseWriter, r *http.Request) {
	type request struct {
		TemplateID string `json:"template_id"`
	}
	var body request
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
		return
	}
	principal := principalFromContext(r.Context())
	assignment, err := h.service.AssignTemplate(
		r.Context(), principal.AccountID, r.PathValue("connectionID"), body.TemplateID,
	)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, assignment)
}

func (h *CheckinHandler) listAssignmentsForProfessional(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	assignments, err := h.service.ListAssignmentsForProfessional(
		r.Context(), principal.AccountID, r.PathValue("connectionID"),
	)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"assignments": assignments})
}

func (h *CheckinHandler) revokeAssignment(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	if err := h.service.RevokeAssignment(
		r.Context(), principal.AccountID,
		r.PathValue("connectionID"), r.PathValue("assignmentID"),
	); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *CheckinHandler) createCollectionRequest(w http.ResponseWriter, r *http.Request) {
	type request struct {
		PeriodStart string `json:"period_start"`
		PeriodEnd   string `json:"period_end"`
	}
	var body request
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
		return
	}
	principal := principalFromContext(r.Context())
	collectionRequest, err := h.service.CreateCollectionRequest(
		r.Context(), principal.AccountID, r.PathValue("connectionID"),
		body.PeriodStart, body.PeriodEnd,
	)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, collectionRequest)
}

func (h *CheckinHandler) listCollectionRequestsForProfessional(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	requests, err := h.service.ListCollectionRequestsForProfessional(
		r.Context(), principal.AccountID, r.PathValue("connectionID"),
	)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": requests})
}

func (h *CheckinHandler) listCollections(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	collections, err := h.service.ListCollections(
		r.Context(), principal.AccountID, r.PathValue("connectionID"),
	)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"collections": collections})
}

/* ------------------------------------------------------------ paciente */

func (h *CheckinHandler) listCheckins(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	assignments, err := h.service.ListAssignmentsForApp(
		r.Context(), principal.AccountID, "",
		statusFilter(r.URL.Query().Get("status"), []string{"active"}),
		r.URL.Query().Get("date"),
	)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"checkins": assignments})
}

func (h *CheckinHandler) listAssignmentsForApp(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	assignments, err := h.service.ListAssignmentsForApp(
		r.Context(), principal.AccountID, r.PathValue("connectionID"),
		statusFilter(r.URL.Query().Get("status"), []string{"pending", "active"}),
		r.URL.Query().Get("date"),
	)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"assignments": assignments})
}

func (h *CheckinHandler) submitEntry(w http.ResponseWriter, r *http.Request) {
	type request struct {
		EntryDate string `json:"entry_date"`
		Answers   []struct {
			QuestionID string `json:"question_id"`
			OptionID   string `json:"option_id"`
		} `json:"answers"`
	}
	var body request
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
		return
	}
	answers := make([]checkin.AnswerInput, 0, len(body.Answers))
	for _, answer := range body.Answers {
		answers = append(answers, checkin.AnswerInput{
			QuestionID: answer.QuestionID, OptionID: answer.OptionID,
		})
	}
	principal := principalFromContext(r.Context())
	entry, err := h.service.SubmitEntry(
		r.Context(), principal.AccountID, r.PathValue("assignmentID"),
		body.EntryDate, answers, r.Header.Get("Idempotency-Key"),
	)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, entry)
}

func (h *CheckinHandler) listEntries(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	entries, err := h.service.ListEntries(
		r.Context(), principal.AccountID, r.PathValue("assignmentID"),
		r.URL.Query().Get("from"), r.URL.Query().Get("to"),
	)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func (h *CheckinHandler) acceptAssignment(w http.ResponseWriter, r *http.Request) {
	h.respondToAssignment(w, r, true)
}

func (h *CheckinHandler) declineAssignment(w http.ResponseWriter, r *http.Request) {
	h.respondToAssignment(w, r, false)
}

func (h *CheckinHandler) respondToAssignment(
	w http.ResponseWriter,
	r *http.Request,
	accepted bool,
) {
	principal := principalFromContext(r.Context())
	if err := h.service.RespondToAssignment(
		r.Context(), principal.AccountID, r.PathValue("assignmentID"), accepted,
	); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *CheckinHandler) endAssignment(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	if err := h.service.EndAssignment(
		r.Context(), principal.AccountID, r.PathValue("assignmentID"),
	); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *CheckinHandler) listCollectionRequestsForApp(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	requests, err := h.service.ListCollectionRequestsForApp(
		r.Context(), principal.AccountID, r.PathValue("connectionID"),
	)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": requests})
}

func (h *CheckinHandler) sendCollection(w http.ResponseWriter, r *http.Request) {
	type request struct {
		AssignmentIDs []string `json:"assignment_ids"`
	}
	var body request
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
		return
	}
	principal := principalFromContext(r.Context())
	result, err := h.service.SendCollection(
		r.Context(), principal.AccountID, r.PathValue("requestID"), body.AssignmentIDs,
	)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *CheckinHandler) declineCollectionRequest(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	if err := h.service.DeclineCollectionRequest(
		r.Context(), principal.AccountID, r.PathValue("requestID"),
	); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// statusFilter aceita "a,b" na query e cai no padrão quando vazio. Valores
// inválidos são recusados no serviço, não aqui: o filtro é dado, e dado do
// cliente se valida onde a regra mora.
func statusFilter(value string, fallback []string) []string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	parts := strings.Split(value, ",")
	statuses := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			statuses = append(statuses, trimmed)
		}
	}
	if len(statuses) == 0 {
		return fallback
	}
	return statuses
}

func (h *CheckinHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, checkin.ErrInvalidInput):
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", "check-in payload is invalid")
	case errors.Is(err, checkin.ErrForbidden):
		writeError(w, http.StatusForbidden, "checkin_forbidden", "this check-in is not accessible")
	case errors.Is(err, checkin.ErrTemplatePublished):
		writeError(w, http.StatusConflict, "checkin_template_published", "a published check-in cannot be edited")
	case errors.Is(err, checkin.ErrTooManyAssignments):
		writeError(w, http.StatusConflict, "checkin_limit_reached", "this connection already has the maximum of open check-ins")
	case errors.Is(err, checkin.ErrRequestResolved):
		writeError(w, http.StatusConflict, "checkin_request_resolved", "this request was already answered")
	case errors.Is(err, checkin.ErrConflict):
		writeError(w, http.StatusConflict, "checkin_conflict", "this check-in already has an open request")
	case errors.Is(err, checkin.ErrNoEntries):
		writeError(w, http.StatusUnprocessableEntity, "checkin_no_entries", "the selected period has no answered days")
	case errors.Is(err, checkin.ErrSubscriptionRequired):
		writeError(w, http.StatusPaymentRequired, "subscription_required", "an active professional subscription is required")
	case errors.Is(err, checkin.ErrProfileIncomplete):
		writeError(w, http.StatusConflict, "profile_incomplete", "professional profile must be completed first")
	case errors.Is(err, checkin.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "check-in was not found")
	default:
		slog.Error("check-in request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
	}
}
