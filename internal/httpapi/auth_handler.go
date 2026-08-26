package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Vini-create/psycho-app-back/internal/auth"
)

type AuthHandlerConfig struct {
	CookieSecure    bool
	RefreshTokenTTL time.Duration
	AllowedOrigins  []string
}

type AuthHandler struct {
	service              *auth.Service
	config               AuthHandlerConfig
	allowedOrigins       map[string]struct{}
	loginIPLimiter       *fixedWindowLimiter
	loginIdentityLimiter *fixedWindowLimiter
	tokenIPLimiter       *fixedWindowLimiter
	tokenIdentityLimiter *fixedWindowLimiter
	devicePollLimiter    *fixedWindowLimiter
}

func NewAuthHandler(service *auth.Service, config AuthHandlerConfig) *AuthHandler {
	allowedOrigins := make(map[string]struct{}, len(config.AllowedOrigins))
	for _, origin := range config.AllowedOrigins {
		allowedOrigins[origin] = struct{}{}
	}

	return &AuthHandler{
		service:              service,
		config:               config,
		allowedOrigins:       allowedOrigins,
		loginIPLimiter:       newFixedWindowLimiter(30, time.Minute),
		loginIdentityLimiter: newFixedWindowLimiter(10, time.Minute),
		tokenIPLimiter:       newFixedWindowLimiter(20, time.Minute),
		tokenIdentityLimiter: newFixedWindowLimiter(5, time.Minute),
		devicePollLimiter:    newFixedWindowLimiter(45, time.Minute),
	}
}

func (h *AuthHandler) RegisterRoutes(mux *http.ServeMux, audience auth.Audience) {
	prefix := "/v1/" + string(audience)
	authPrefix := prefix + "/auth"

	mux.HandleFunc("POST "+authPrefix+"/register", h.register(audience))
	mux.HandleFunc("POST "+authPrefix+"/login", h.login(audience))
	mux.HandleFunc("POST "+authPrefix+"/google/challenge", h.googleChallenge(audience))
	mux.HandleFunc("POST "+authPrefix+"/google", h.googleLogin(audience))
	mux.HandleFunc("POST "+authPrefix+"/refresh", h.refresh(audience))
	mux.HandleFunc("POST "+authPrefix+"/logout", h.requireAuth(audience, h.logout(audience)))
	mux.HandleFunc("POST "+authPrefix+"/logout-all", h.requireAuth(audience, h.logoutAll(audience)))
	mux.HandleFunc("POST "+authPrefix+"/email-verification/request", h.requestEmailVerification(audience))
	mux.HandleFunc("POST "+authPrefix+"/email-verification/confirm", h.confirmEmail(audience))
	mux.HandleFunc("POST "+authPrefix+"/password-reset/request", h.requestPasswordReset(audience))
	mux.HandleFunc("POST "+authPrefix+"/password-reset/confirm", h.confirmPasswordReset(audience))
	mux.HandleFunc("GET "+authPrefix+"/sessions", h.requireAuth(audience, h.listSessions))
	mux.HandleFunc("DELETE "+authPrefix+"/sessions/{sessionID}", h.requireAuth(audience, h.revokeSession))
	mux.HandleFunc("GET "+prefix+"/me", h.requireAuth(audience, h.me))
	mux.HandleFunc("PATCH "+prefix+"/me", h.requireAuth(audience, h.updateMe))
	mux.HandleFunc("PUT "+authPrefix+"/password", h.requireAuth(audience, h.changePassword))

	if audience == auth.AudienceProfessional {
		mux.HandleFunc(
			"POST "+authPrefix+"/passkeys/registration/options",
			h.requireAuth(audience, h.beginPasskeyRegistration),
		)
		mux.HandleFunc(
			"POST "+authPrefix+"/passkeys/registration/verify",
			h.requireAuth(audience, h.finishPasskeyRegistration),
		)
		mux.HandleFunc(
			"POST "+authPrefix+"/passkeys/authentication/verify",
			h.finishPasskeyAuthentication,
		)
		mux.HandleFunc(
			"POST "+authPrefix+"/passkeys/authentication/recovery",
			h.recoverPasskeyAuthentication,
		)
		mux.HandleFunc(
			"POST "+authPrefix+"/device-authorizations/preview",
			h.previewDeviceAuthorization,
		)
		mux.HandleFunc(
			"POST "+authPrefix+"/device-authorizations/approve",
			h.approveDeviceAuthorization,
		)
		mux.HandleFunc(
			"POST "+authPrefix+"/device-authorizations/consume",
			h.consumeDeviceAuthorization,
		)
		mux.HandleFunc(
			"GET "+authPrefix+"/passkeys",
			h.requireAuth(audience, h.listPasskeys),
		)
		mux.HandleFunc(
			"DELETE "+authPrefix+"/passkeys/{passkeyID}",
			h.requireAuth(audience, requireMFA(h.removePasskey)),
		)
		mux.HandleFunc(
			"POST "+authPrefix+"/passkeys/recovery-codes/regenerate",
			h.requireAuth(audience, requireMFA(h.regenerateRecoveryCodes)),
		)
	}
}

func (h *AuthHandler) register(audience auth.Audience) http.HandlerFunc {
	type request struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		var body request
		if err := readJSON(w, r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
			return
		}

		result, err := h.service.Register(r.Context(), audience, auth.RegisterInput{
			Email: body.Email, Password: body.Password, DisplayName: body.DisplayName,
		}, clientInfo(r))
		if err != nil {
			h.handleServiceError(w, err)
			return
		}

		writeJSON(w, http.StatusCreated, result)
	}
}

func (h *AuthHandler) login(audience auth.Audience) http.HandlerFunc {
	type request struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		var body request
		if err := readJSON(w, r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
			return
		}

		if !h.loginAllowed(r, audience, body.Email) {
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, "rate_limited", "too many authentication attempts")
			return
		}

		result, err := h.service.Login(
			r.Context(), audience, body.Email, body.Password, clientInfo(r),
		)
		if err != nil {
			h.handleServiceError(w, err)
			return
		}

		if result.Tokens != nil {
			h.setRefreshCookie(w, audience, result.Tokens.RefreshToken)
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func (h *AuthHandler) googleChallenge(audience auth.Audience) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		challenge, err := h.service.BeginGoogleLogin(r.Context(), audience, clientInfo(r))
		if err != nil {
			h.handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, challenge)
	}
}

func (h *AuthHandler) googleLogin(audience auth.Audience) http.HandlerFunc {
	type request struct {
		ChallengeID string `json:"challenge_id"`
		Credential  string `json:"credential"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		var body request
		if err := readJSON(w, r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
			return
		}
		if !h.loginAllowed(r, audience, "google:"+body.ChallengeID) {
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, "rate_limited", "too many authentication attempts")
			return
		}
		result, err := h.service.LoginWithGoogle(
			r.Context(), audience, body.ChallengeID, body.Credential, clientInfo(r),
		)
		if err != nil {
			h.handleServiceError(w, err)
			return
		}
		if result.Tokens != nil {
			h.setRefreshCookie(w, audience, result.Tokens.RefreshToken)
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func (h *AuthHandler) refresh(audience auth.Audience) http.HandlerFunc {
	type request struct {
		RefreshToken string `json:"refresh_token"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		rawToken := ""
		cookie, cookieErr := r.Cookie(refreshCookieName(audience))
		if cookieErr == nil {
			if !h.originAllowed(r) {
				writeError(w, http.StatusForbidden, "origin_not_allowed", "request origin is not allowed")
				return
			}
			rawToken = cookie.Value
		} else if r.ContentLength != 0 {
			var body request
			if err := readJSON(w, r, &body); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
				return
			}
			rawToken = body.RefreshToken
		}

		result, err := h.service.Refresh(r.Context(), audience, rawToken, clientInfo(r))
		if err != nil {
			h.clearRefreshCookie(w, audience)
			h.handleServiceError(w, err)
			return
		}

		h.setRefreshCookie(w, audience, result.RefreshToken)
		writeJSON(w, http.StatusOK, result)
	}
}

func (h *AuthHandler) logout(audience auth.Audience) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal := principalFromContext(r.Context())
		if err := h.service.RevokeSession(
			r.Context(), principal, principal.SessionID, clientInfo(r),
		); err != nil && !errors.Is(err, auth.ErrNotFound) {
			h.handleServiceError(w, err)
			return
		}
		h.clearRefreshCookie(w, audience)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *AuthHandler) logoutAll(audience auth.Audience) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h.service.LogoutAll(
			r.Context(), principalFromContext(r.Context()), clientInfo(r),
		); err != nil {
			h.handleServiceError(w, err)
			return
		}
		h.clearRefreshCookie(w, audience)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *AuthHandler) requestEmailVerification(audience auth.Audience) http.HandlerFunc {
	return h.tokenRequestHandler(audience, h.service.RequestEmailVerification)
}

func (h *AuthHandler) requestPasswordReset(audience auth.Audience) http.HandlerFunc {
	return h.tokenRequestHandler(audience, h.service.RequestPasswordReset)
}

func (h *AuthHandler) tokenRequestHandler(
	audience auth.Audience,
	operation func(
		context.Context,
		auth.Audience,
		string,
		auth.ClientInfo,
	) (auth.OneTimeTokenResult, error),
) http.HandlerFunc {
	type request struct {
		Email string `json:"email"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		var body request
		if err := readJSON(w, r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
			return
		}
		if !h.tokenRequestAllowed(r, audience, body.Email) {
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, "rate_limited", "too many token requests")
			return
		}

		result, err := operation(r.Context(), audience, body.Email, clientInfo(r))
		if err != nil {
			h.handleServiceError(w, err)
			return
		}

		response := map[string]any{
			"message": "if the account exists, instructions will be sent",
		}
		if result.DevelopmentToken != "" {
			response["development_token"] = result.DevelopmentToken
		}
		writeJSON(w, http.StatusAccepted, response)
	}
}

func (h *AuthHandler) confirmEmail(audience auth.Audience) http.HandlerFunc {
	type request struct {
		Token string `json:"token"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var body request
		if err := readJSON(w, r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
			return
		}
		if err := h.service.ConfirmEmail(r.Context(), audience, body.Token, clientInfo(r)); err != nil {
			h.handleServiceError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *AuthHandler) confirmPasswordReset(audience auth.Audience) http.HandlerFunc {
	type request struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var body request
		if err := readJSON(w, r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
			return
		}
		if err := h.service.CompletePasswordReset(
			r.Context(), audience, body.Token, body.NewPassword, clientInfo(r),
		); err != nil {
			h.handleServiceError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *AuthHandler) listSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := h.service.ListSessions(r.Context(), principalFromContext(r.Context()))
	if err != nil {
		h.handleServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

func (h *AuthHandler) revokeSession(w http.ResponseWriter, r *http.Request) {
	if err := h.service.RevokeSession(
		r.Context(),
		principalFromContext(r.Context()),
		r.PathValue("sessionID"),
		clientInfo(r),
	); err != nil {
		h.handleServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *AuthHandler) me(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())
	account, err := h.service.Account(r.Context(), principal)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}
	h.writeAccount(w, http.StatusOK, account, principal)
}

func (h *AuthHandler) updateMe(w http.ResponseWriter, r *http.Request) {
	type request struct {
		DisplayName string `json:"display_name"`
	}
	var body request
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
		return
	}
	principal := principalFromContext(r.Context())
	account, err := h.service.UpdateDisplayName(
		r.Context(), principal, body.DisplayName, clientInfo(r),
	)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}
	h.writeAccount(w, http.StatusOK, account, principal)
}

func (h *AuthHandler) changePassword(w http.ResponseWriter, r *http.Request) {
	type request struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	var body request
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
		return
	}
	if err := h.service.ChangePassword(
		r.Context(), principalFromContext(r.Context()),
		body.CurrentPassword, body.NewPassword, clientInfo(r),
	); err != nil {
		h.handleServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *AuthHandler) writeAccount(
	w http.ResponseWriter,
	status int,
	account auth.Account,
	principal auth.Principal,
) {
	response := map[string]any{
		"id": account.ID, "email": account.Email, "display_name": account.DisplayName,
		"status": account.Status, "email_verified_at": account.EmailVerifiedAt,
		"audience": principal.Audience, "mfa_verified": principal.MFA,
		"created_at": account.CreatedAt, "updated_at": account.UpdatedAt,
		"google_connected": account.GoogleConnected,
	}
	if account.Plan != "" {
		response["plan"] = account.Plan
	}
	writeJSON(w, status, response)
}

func (h *AuthHandler) beginPasskeyRegistration(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.BeginPasskeyRegistration(
		r.Context(), principalFromContext(r.Context()), clientInfo(r),
	)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *AuthHandler) finishPasskeyRegistration(w http.ResponseWriter, r *http.Request) {
	type request struct {
		CeremonyToken string          `json:"ceremony_token"`
		Label         string          `json:"label"`
		Credential    json.RawMessage `json:"credential"`
	}
	var body request
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
		return
	}
	result, err := h.service.FinishPasskeyRegistration(
		r.Context(),
		principalFromContext(r.Context()),
		body.CeremonyToken,
		body.Credential,
		body.Label,
		clientInfo(r),
	)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (h *AuthHandler) finishPasskeyAuthentication(w http.ResponseWriter, r *http.Request) {
	type request struct {
		CeremonyToken string          `json:"ceremony_token"`
		Credential    json.RawMessage `json:"credential"`
	}
	var body request
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
		return
	}
	if !h.loginAllowed(r, auth.AudienceProfessional, body.CeremonyToken) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many authentication attempts")
		return
	}
	result, err := h.service.FinishPasskeyAuthentication(
		r.Context(), body.CeremonyToken, body.Credential, clientInfo(r),
	)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}
	h.setRefreshCookie(w, auth.AudienceProfessional, result.RefreshToken)
	writeJSON(w, http.StatusOK, result)
}

func (h *AuthHandler) recoverPasskeyAuthentication(w http.ResponseWriter, r *http.Request) {
	type request struct {
		CeremonyToken string `json:"ceremony_token"`
		RecoveryCode  string `json:"recovery_code"`
	}
	var body request
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
		return
	}
	if !h.loginAllowed(r, auth.AudienceProfessional, body.CeremonyToken) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many authentication attempts")
		return
	}
	result, err := h.service.RecoverPasskeyAuthentication(
		r.Context(), body.CeremonyToken, body.RecoveryCode, clientInfo(r),
	)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}
	h.setRefreshCookie(w, auth.AudienceProfessional, result.RefreshToken)
	writeJSON(w, http.StatusOK, result)
}

func (h *AuthHandler) previewDeviceAuthorization(w http.ResponseWriter, r *http.Request) {
	type request struct {
		ScanToken string `json:"scan_token"`
	}
	var body request
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
		return
	}
	if !h.loginAllowed(r, auth.AudienceProfessional, body.ScanToken) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many authorization attempts")
		return
	}
	result, err := h.service.PreviewDeviceAuthorization(r.Context(), body.ScanToken)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *AuthHandler) approveDeviceAuthorization(w http.ResponseWriter, r *http.Request) {
	type request struct {
		ScanToken  string          `json:"scan_token"`
		Credential json.RawMessage `json:"credential"`
	}
	var body request
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
		return
	}
	if !h.loginAllowed(r, auth.AudienceProfessional, body.ScanToken) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many authorization attempts")
		return
	}
	if err := h.service.ApproveDeviceAuthorization(
		r.Context(), body.ScanToken, body.Credential, clientInfo(r),
	); err != nil {
		h.handleServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *AuthHandler) consumeDeviceAuthorization(w http.ResponseWriter, r *http.Request) {
	type request struct {
		PollToken string `json:"poll_token"`
	}
	var body request
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body contains invalid JSON")
		return
	}
	_, identityKey := rateLimitKeys(r, auth.AudienceProfessional, body.PollToken)
	if !h.devicePollLimiter.Allow(identityKey) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many authorization checks")
		return
	}
	result, err := h.service.ConsumeDeviceAuthorization(
		r.Context(), body.PollToken, clientInfo(r),
	)
	if errors.Is(err, auth.ErrDeviceAuthorizationPending) {
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "pending"})
		return
	}
	if err != nil {
		h.handleServiceError(w, err)
		return
	}
	h.setRefreshCookie(w, auth.AudienceProfessional, result.RefreshToken)
	writeJSON(w, http.StatusOK, result)
}

func (h *AuthHandler) listPasskeys(w http.ResponseWriter, r *http.Request) {
	passkeys, err := h.service.ListPasskeys(
		r.Context(), principalFromContext(r.Context()),
	)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"passkeys": passkeys})
}

func (h *AuthHandler) removePasskey(w http.ResponseWriter, r *http.Request) {
	if err := h.service.RemovePasskey(
		r.Context(),
		principalFromContext(r.Context()),
		r.PathValue("passkeyID"),
		clientInfo(r),
	); err != nil {
		h.handleServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *AuthHandler) regenerateRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.RegenerateRecoveryCodes(
		r.Context(), principalFromContext(r.Context()), clientInfo(r),
	)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *AuthHandler) handleServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrInvalidInput), errors.Is(err, auth.ErrWeakPassword):
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
	case errors.Is(err, auth.ErrPasswordUnchanged):
		writeError(w, http.StatusUnprocessableEntity, "password_unchanged", err.Error())
	case errors.Is(err, auth.ErrConflict):
		writeError(w, http.StatusConflict, "account_exists", "an account with this email already exists")
	case errors.Is(err, auth.ErrPasskeyExists):
		writeError(w, http.StatusConflict, "passkey_exists", "this passkey is already registered")
	case errors.Is(err, auth.ErrLastPasskey):
		writeError(w, http.StatusConflict, "last_passkey", "register another passkey before removing this one")
	case errors.Is(err, auth.ErrPasskeyLimit):
		writeError(w, http.StatusConflict, "passkey_limit", "the account reached the passkey limit")
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "email or password is incorrect")
	case errors.Is(err, auth.ErrInvalidGoogleCredential):
		writeError(w, http.StatusUnauthorized, "invalid_google_credential", "Google authentication is invalid or expired")
	case errors.Is(err, auth.ErrGoogleNotConfigured):
		writeError(w, http.StatusServiceUnavailable, "google_not_configured", "Google authentication is unavailable")
	case errors.Is(err, auth.ErrGoogleLinkRequired):
		writeError(w, http.StatusConflict, "google_link_required", "this account must use email and password")
	case errors.Is(err, auth.ErrEmailNotVerified):
		writeError(w, http.StatusForbidden, "email_not_verified", "email verification is required")
	case errors.Is(err, auth.ErrAccountUnavailable):
		writeError(w, http.StatusForbidden, "account_unavailable", "account is unavailable")
	case errors.Is(err, auth.ErrInvalidToken), errors.Is(err, auth.ErrInvalidRefreshToken):
		writeError(w, http.StatusUnauthorized, "invalid_token", "token is invalid or expired")
	case errors.Is(err, auth.ErrInvalidRecoveryCode):
		writeError(w, http.StatusUnauthorized, "invalid_recovery_code", "recovery code is invalid or already used")
	case errors.Is(err, auth.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "resource was not found")
	case errors.Is(err, auth.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "operation is not allowed")
	default:
		slog.Error("auth request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
	}
}

func (h *AuthHandler) setRefreshCookie(w http.ResponseWriter, audience auth.Audience, token string) {
	// #nosec G124 -- Secure may be false only for localhost development; config
	// loading requires it to be true in every other environment.
	cookie := &http.Cookie{
		Name: refreshCookieName(audience), Value: token,
		Path: "/v1/" + string(audience) + "/auth", MaxAge: int(h.config.RefreshTokenTTL.Seconds()),
		HttpOnly: true, Secure: h.config.CookieSecure, SameSite: http.SameSiteLaxMode,
	}
	h.configureRefreshCookieSite(cookie)
	http.SetCookie(w, cookie)
}

func (h *AuthHandler) clearRefreshCookie(w http.ResponseWriter, audience auth.Audience) {
	// #nosec G124 -- deletion must use the same development/production cookie
	// attributes as creation; production configuration enforces Secure=true.
	cookie := &http.Cookie{
		Name: refreshCookieName(audience), Value: "",
		Path: "/v1/" + string(audience) + "/auth", MaxAge: -1,
		HttpOnly: true, Secure: h.config.CookieSecure, SameSite: http.SameSiteLaxMode,
	}
	h.configureRefreshCookieSite(cookie)
	http.SetCookie(w, cookie)
}

func (h *AuthHandler) configureRefreshCookieSite(cookie *http.Cookie) {
	if !h.config.CookieSecure {
		return
	}
	// O frontend provisório (workers.dev) e a API (railway.app) são sites
	// diferentes. None permite o envio credentialed e Partitioned mantém o
	// cookie isolado pelo site de topo mesmo em navegadores que bloqueiam
	// cookies de terceiros. CORS e originAllowed continuam limitando quem pode
	// acionar a rotação.
	cookie.SameSite = http.SameSiteNoneMode
	cookie.Partitioned = true
}

func (h *AuthHandler) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	_, allowed := h.allowedOrigins[origin]
	return allowed
}

func refreshCookieName(audience auth.Audience) string {
	return "anamnesys_" + string(audience) + "_refresh"
}

func clientInfo(r *http.Request) auth.ClientInfo {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	userAgent := r.UserAgent()
	if utf8.RuneCountInString(userAgent) > 1024 {
		userAgent = string([]rune(userAgent)[:1024])
	}
	return auth.ClientInfo{IPAddress: host, UserAgent: userAgent}
}

func rateLimitKeys(r *http.Request, audience auth.Audience, identifier string) (string, string) {
	client := clientInfo(r)
	digest := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(identifier))))
	prefix := string(audience) + ":"
	return prefix + "ip:" + client.IPAddress, prefix + "identity:" + hex.EncodeToString(digest[:])
}

func (h *AuthHandler) loginAllowed(
	r *http.Request,
	audience auth.Audience,
	identifier string,
) bool {
	ipKey, identityKey := rateLimitKeys(r, audience, identifier)
	return h.loginIPLimiter.Allow(ipKey) && h.loginIdentityLimiter.Allow(identityKey)
}

func (h *AuthHandler) tokenRequestAllowed(
	r *http.Request,
	audience auth.Audience,
	identifier string,
) bool {
	ipKey, identityKey := rateLimitKeys(r, audience, identifier)
	return h.tokenIPLimiter.Allow(ipKey) && h.tokenIdentityLimiter.Allow(identityKey)
}
