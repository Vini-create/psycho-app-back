package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/Vini-create/psycho-app-back/internal/auth"
	"github.com/Vini-create/psycho-app-back/internal/care"
)

type CareHandler struct {
	service *care.Service
}

func NewCareHandler(service *care.Service) *CareHandler {
	return &CareHandler{service: service}
}

func (h *CareHandler) RegisterRoutes(mux *http.ServeMux, authHandler *AuthHandler) {
	requireApp := func(handler http.HandlerFunc) http.HandlerFunc {
		return authHandler.requireAuth(auth.AudienceApp, handler)
	}
	requireProfessional := func(handler http.HandlerFunc) http.HandlerFunc {
		return authHandler.requireAuth(auth.AudienceProfessional, requireMFA(handler))
	}

	mux.HandleFunc("GET /v1/app/invitations/{token}", h.previewInvitation)
	mux.HandleFunc("POST /v1/app/invitations/{token}/accept", requireApp(h.acceptInvitation))
	mux.HandleFunc("GET /v1/app/connections", requireApp(h.listAppConnections))
	mux.HandleFunc("PUT /v1/app/connections/{connectionID}/consents", requireApp(h.replaceConnectionConsents))
	mux.HandleFunc("DELETE /v1/app/connections/{connectionID}", requireApp(h.endAppConnection))

	mux.HandleFunc("GET /v1/professional/profile", requireProfessional(h.getProfessionalProfile))
	mux.HandleFunc("PUT /v1/professional/profile", requireProfessional(h.upsertProfessionalProfile))
	mux.HandleFunc("GET /v1/professional/invitations", requireProfessional(h.listInvitations))
	mux.HandleFunc("POST /v1/professional/invitations", requireProfessional(h.createInvitation))
	mux.HandleFunc("DELETE /v1/professional/invitations/{invitationID}", requireProfessional(h.revokeInvitation))
	mux.HandleFunc("GET /v1/professional/patients", requireProfessional(h.listProfessionalPatients))
	mux.HandleFunc("GET /v1/professional/patients/{connectionID}", requireProfessional(h.getProfessionalPatient))
	mux.HandleFunc("POST /v1/professional/patients/{connectionID}/end", requireProfessional(h.endProfessionalConnection))
}

func (h *CareHandler) getProfessionalProfile(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	profile, err := h.service.GetProfessionalProfile(r.Context(), principal.AccountID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (h *CareHandler) upsertProfessionalProfile(w http.ResponseWriter, r *http.Request) {
	type request struct {
		ProfessionType          string   `json:"profession_type"`
		RegistrationCountryCode string   `json:"registration_country_code"`
		RegistrationRegion      string   `json:"registration_region"`
		RegistrationNumber      string   `json:"registration_number"`
		Bio                     string   `json:"bio"`
		Certifications          []string `json:"certifications"`
	}
	var body request
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
		return
	}
	principal := principalFromContext(r.Context())
	profile, err := h.service.UpsertProfessionalProfile(
		r.Context(),
		principal.AccountID,
		care.ProfessionalProfileInput{
			ProfessionType:          body.ProfessionType,
			RegistrationCountryCode: body.RegistrationCountryCode,
			RegistrationRegion:      body.RegistrationRegion,
			RegistrationNumber:      body.RegistrationNumber,
			Bio:                     body.Bio,
			Certifications:          body.Certifications,
		},
	)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (h *CareHandler) createInvitation(w http.ResponseWriter, r *http.Request) {
	type request struct {
		Email string `json:"email"`
	}
	var body request
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
		return
	}
	principal := principalFromContext(r.Context())
	invitation, err := h.service.CreateInvitation(r.Context(), principal.AccountID, body.Email)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, invitation)
}

func (h *CareHandler) listInvitations(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	invitations, err := h.service.ListInvitations(r.Context(), principal.AccountID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"invitations": invitations})
}

func (h *CareHandler) revokeInvitation(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	if err := h.service.RevokeInvitation(
		r.Context(), principal.AccountID, r.PathValue("invitationID"),
	); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *CareHandler) previewInvitation(w http.ResponseWriter, r *http.Request) {
	preview, err := h.service.PreviewInvitation(r.Context(), r.PathValue("token"))
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (h *CareHandler) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	type request struct {
		ConsentScopes []string `json:"consent_scopes"`
	}
	var body request
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
		return
	}
	principal := principalFromContext(r.Context())
	requestClient := clientInfo(r)
	connectionID, err := h.service.AcceptInvitation(
		r.Context(),
		principal.AccountID,
		r.PathValue("token"),
		body.ConsentScopes,
		care.ClientInfo{IPAddress: requestClient.IPAddress, UserAgent: requestClient.UserAgent},
	)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"connection_id": connectionID, "status": "active"})
}

func (h *CareHandler) listAppConnections(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	connections, err := h.service.ListAppConnections(r.Context(), principal.AccountID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": connections})
}

func (h *CareHandler) replaceConnectionConsents(w http.ResponseWriter, r *http.Request) {
	type request struct {
		ConsentScopes []string `json:"consent_scopes"`
	}
	var body request
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
		return
	}
	principal := principalFromContext(r.Context())
	requestClient := clientInfo(r)
	if err := h.service.ReplaceConnectionConsents(
		r.Context(),
		principal.AccountID,
		r.PathValue("connectionID"),
		body.ConsentScopes,
		care.ClientInfo{IPAddress: requestClient.IPAddress, UserAgent: requestClient.UserAgent},
	); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *CareHandler) endAppConnection(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	if err := h.service.EndAppConnection(
		r.Context(), principal.AccountID, r.PathValue("connectionID"),
	); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *CareHandler) listProfessionalPatients(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	patients, err := h.service.ListProfessionalPatients(r.Context(), principal.AccountID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"patients": patients})
}

func (h *CareHandler) getProfessionalPatient(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	patient, err := h.service.GetProfessionalPatient(
		r.Context(), principal.AccountID, r.PathValue("connectionID"),
	)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, patient)
}

func (h *CareHandler) endProfessionalConnection(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	if err := h.service.EndProfessionalConnection(
		r.Context(), principal.AccountID, r.PathValue("connectionID"),
	); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *CareHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, care.ErrInvalidInput):
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", "request values are invalid")
	case errors.Is(err, care.ErrInvalidToken):
		writeError(w, http.StatusGone, "invitation_invalid", "invitation is invalid, expired or already used")
	case errors.Is(err, care.ErrConflict):
		writeError(w, http.StatusConflict, "connection_exists", "an open connection already exists")
	case errors.Is(err, care.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "operation is not allowed")
	case errors.Is(err, care.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "resource was not found")
	default:
		slog.Error("care request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
	}
}
