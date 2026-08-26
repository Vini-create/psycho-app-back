package httpapi

import (
	"net/http"

	"github.com/Vini-create/psycho-app-back/internal/auth"
)

func NewRouter(
	authHandler *AuthHandler,
	chatHandler *ChatHandler,
	careHandler *CareHandler,
	insightHandler *InsightHandler,
	checkinHandler *CheckinHandler,
	allowedOrigins []string,
) http.Handler {
	mux := newMux()
	authHandler.RegisterRoutes(mux, auth.AudienceApp)
	authHandler.RegisterRoutes(mux, auth.AudienceProfessional)
	chatHandler.RegisterRoutes(mux, authHandler)
	careHandler.RegisterRoutes(mux, authHandler)
	insightHandler.RegisterRoutes(mux, authHandler)
	checkinHandler.RegisterRoutes(mux, authHandler)

	return securityHeaders(corsMiddleware(mux, allowedOrigins))
}

func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler)
	return mux
}
