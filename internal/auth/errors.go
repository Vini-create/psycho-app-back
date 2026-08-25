package auth

import "errors"

var (
	ErrConflict                = errors.New("resource already exists")
	ErrInvalidCredentials      = errors.New("invalid credentials")
	ErrEmailNotVerified        = errors.New("email is not verified")
	ErrAccountUnavailable      = errors.New("account is unavailable")
	ErrInvalidToken            = errors.New("invalid or expired token")
	ErrInvalidRefreshToken     = errors.New("invalid refresh token")
	ErrInvalidRecoveryCode     = errors.New("invalid recovery code")
	ErrPasskeyExists           = errors.New("passkey already exists")
	ErrLastPasskey             = errors.New("last passkey cannot be removed")
	ErrPasskeyLimit            = errors.New("passkey limit reached")
	ErrForbidden               = errors.New("operation is forbidden")
	ErrNotFound                = errors.New("resource not found")
	ErrWeakPassword            = errors.New("password does not meet security requirements")
	ErrPasswordUnchanged       = errors.New("new password must be different from current password")
	ErrInvalidInput            = errors.New("invalid input")
	ErrInvalidGoogleCredential = errors.New("invalid Google credential")
	ErrGoogleNotConfigured     = errors.New("Google login is not configured")
	ErrGoogleLinkRequired      = errors.New("existing account requires explicit Google linking")
)
