package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Vini-create/psycho-app-back/internal/auth"
)

func TestRefreshCookieUsesLaxModeOnLocalDevelopment(t *testing.T) {
	handler := &AuthHandler{config: AuthHandlerConfig{
		CookieSecure: false, RefreshTokenTTL: 30 * 24 * time.Hour,
	}}
	recorder := httptest.NewRecorder()
	handler.setRefreshCookie(recorder, auth.AudienceApp, "refresh-token")

	cookie := responseCookie(t, recorder)
	if cookie.Secure || cookie.Partitioned || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("development refresh cookie = %#v", cookie)
	}
}

func TestRefreshCookieSupportsCrossSiteProductionFrontend(t *testing.T) {
	handler := &AuthHandler{config: AuthHandlerConfig{
		CookieSecure: true, RefreshTokenTTL: 30 * 24 * time.Hour,
	}}
	recorder := httptest.NewRecorder()
	handler.setRefreshCookie(recorder, auth.AudienceApp, "refresh-token")

	cookie := responseCookie(t, recorder)
	if !cookie.HttpOnly || !cookie.Secure || !cookie.Partitioned ||
		cookie.SameSite != http.SameSiteNoneMode {
		t.Fatalf("production refresh cookie = %#v", cookie)
	}
}

func TestClearedRefreshCookieKeepsProductionAttributes(t *testing.T) {
	handler := &AuthHandler{config: AuthHandlerConfig{
		CookieSecure: true, RefreshTokenTTL: 30 * 24 * time.Hour,
	}}
	recorder := httptest.NewRecorder()
	handler.clearRefreshCookie(recorder, auth.AudienceProfessional)

	cookie := responseCookie(t, recorder)
	if cookie.MaxAge != -1 || !cookie.Secure || !cookie.Partitioned ||
		cookie.SameSite != http.SameSiteNoneMode {
		t.Fatalf("cleared production refresh cookie = %#v", cookie)
	}
}

func responseCookie(t *testing.T, recorder *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("response cookies = %d, want 1", len(cookies))
	}
	return cookies[0]
}
