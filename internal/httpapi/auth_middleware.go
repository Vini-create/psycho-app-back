package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/Vini-create/psycho-app-back/internal/auth"
)

type principalContextKey struct{}

func (h *AuthHandler) requireAuth(audience auth.Audience, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authorization := r.Header.Get("Authorization")
		parts := strings.Fields(authorization)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			writeError(w, http.StatusUnauthorized, "invalid_access_token", "authentication is required")
			return
		}

		principal, err := h.service.Authenticate(r.Context(), audience, parts[1])
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid_access_token", "authentication is required")
			return
		}

		ctx := context.WithValue(r.Context(), principalContextKey{}, principal)
		next(w, r.WithContext(ctx))
	}
}

func requireMFA(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal := principalFromContext(r.Context())
		if !principal.MFA {
			writeError(w, http.StatusForbidden, "mfa_required", "multi-factor authentication is required")
			return
		}
		next(w, r)
	}
}

func principalFromContext(ctx context.Context) auth.Principal {
	principal, _ := ctx.Value(principalContextKey{}).(auth.Principal)
	return principal
}
