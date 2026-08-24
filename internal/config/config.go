package config

import (
	"encoding/base64"
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	App       AppConfig
	HTTP      HTTPConfig
	Database  DatabaseConfig
	Auth      AuthConfig
	Email     EmailConfig
	Companion CompanionConfig
}

type AuthConfig struct {
	Issuer                    string
	JWTPrivateKey             []byte
	DataEncryptionKey         []byte
	AccessTokenTTL            time.Duration
	RefreshTokenTTL           time.Duration
	EmailVerificationTokenTTL time.Duration
	PasswordResetTokenTTL     time.Duration
	GoogleClientID            string
	GoogleChallengeTTL        time.Duration
	CookieSecure              bool
	AllowedOrigins            []string
	ExposeDevelopmentTokens   bool
	WebAuthnRPID              string
	WebAuthnRPDisplayName     string
	WebAuthnOrigins           []string
	WebAuthnCeremonyTTL       time.Duration
}

type EmailConfig struct {
	Provider           string
	BrevoAPIKey        string
	FromName           string
	FromAddress        string
	PatientAppURL      string
	ProfessionalAppURL string
	Timeout            time.Duration
	WorkerEnabled      bool
	WorkerConcurrency  int
	WorkerPoll         time.Duration
	WorkerLease        time.Duration
	WorkerMaxAttempts  int
}

type DatabaseConfig struct {
	URL            string
	ConnectTimeout time.Duration
}

type AppConfig struct {
	Environment          string
	ConsentPolicyVersion string
	InvitationTTL        time.Duration
}

type CompanionConfig struct {
	Enabled                  bool
	BaseURL                  string
	APIKey                   string
	Timeout                  time.Duration
	HistoryMessages          int
	ContextWorkerEnabled     bool
	ContextWorkerConcurrency int
	ContextWorkerPoll        time.Duration
	ContextWorkerLease       time.Duration
	ContextWorkerMaxAttempts int
}

type HTTPConfig struct {
	Address           string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	MaxHeaderBytes    int
	ShutdownTimeout   time.Duration
}

func Load() (Config, error) {
	environment, err := stringFromEnv("APP_ENV")
	if err != nil {
		return Config{}, err
	}

	address, err := stringFromEnv("HTTP_ADDRESS")
	if err != nil {
		return Config{}, err
	}

	readHeaderTimeout, err := durationFromEnv(
		"HTTP_READ_HEADER_TIMEOUT",
	)
	if err != nil {
		return Config{}, err
	}

	readTimeout, err := durationFromEnv("HTTP_READ_TIMEOUT")
	if err != nil {
		return Config{}, err
	}

	writeTimeout, err := durationFromEnv("HTTP_WRITE_TIMEOUT")
	if err != nil {
		return Config{}, err
	}

	idleTimeout, err := durationFromEnv("HTTP_IDLE_TIMEOUT")
	if err != nil {
		return Config{}, err
	}

	maxHeaderBytes, err := intFromEnv("HTTP_MAX_HEADER_BYTES", 1024, 1<<20)
	if err != nil {
		return Config{}, err
	}

	shutdownTimeout, err := durationFromEnv(
		"HTTP_SHUTDOWN_TIMEOUT",
	)
	if err != nil {
		return Config{}, err
	}

	databaseURL, err := stringFromEnv("DATABASE_URL")
	if err != nil {
		return Config{}, err
	}

	databaseConnectTimeout, err := durationFromEnv(
		"DATABASE_CONNECT_TIMEOUT",
	)
	if err != nil {
		return Config{}, err
	}

	consentPolicyVersion, err := stringFromEnv("APP_CONSENT_POLICY_VERSION")
	if err != nil {
		return Config{}, err
	}
	if len(consentPolicyVersion) > 64 {
		return Config{}, fmt.Errorf("APP_CONSENT_POLICY_VERSION must contain at most 64 characters")
	}
	invitationTTL, err := durationFromEnv("APP_INVITATION_TTL")
	if err != nil {
		return Config{}, err
	}

	authConfig, err := loadAuthConfig(environment)
	if err != nil {
		return Config{}, err
	}

	companionConfig, err := loadCompanionConfig(environment)
	if err != nil {
		return Config{}, err
	}
	emailConfig, err := loadEmailConfig(environment)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		App: AppConfig{
			Environment:          environment,
			ConsentPolicyVersion: consentPolicyVersion,
			InvitationTTL:        invitationTTL,
		},
		HTTP: HTTPConfig{
			Address:           address,
			ReadHeaderTimeout: readHeaderTimeout,
			ReadTimeout:       readTimeout,
			WriteTimeout:      writeTimeout,
			IdleTimeout:       idleTimeout,
			MaxHeaderBytes:    maxHeaderBytes,
			ShutdownTimeout:   shutdownTimeout,
		},
		Database: DatabaseConfig{
			URL:            databaseURL,
			ConnectTimeout: databaseConnectTimeout,
		},
		Auth:      authConfig,
		Email:     emailConfig,
		Companion: companionConfig,
	}

	return cfg, nil
}

func loadEmailConfig(environment string) (EmailConfig, error) {
	provider, err := stringFromEnv("EMAIL_PROVIDER")
	if err != nil {
		return EmailConfig{}, err
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider != "mock" && provider != "brevo" {
		return EmailConfig{}, fmt.Errorf("EMAIL_PROVIDER must be mock or brevo")
	}
	if provider == "mock" && environment != "development" {
		return EmailConfig{}, fmt.Errorf("EMAIL_PROVIDER=mock can only be used in development")
	}

	fromName, err := stringFromEnv("EMAIL_FROM_NAME")
	if err != nil {
		return EmailConfig{}, err
	}
	fromAddress, err := stringFromEnv("EMAIL_FROM_ADDRESS")
	if err != nil {
		return EmailConfig{}, err
	}
	if _, err := mail.ParseAddress(fromAddress); err != nil {
		return EmailConfig{}, fmt.Errorf("EMAIL_FROM_ADDRESS must be a valid email address")
	}
	patientURL, err := applicationURLFromEnv("EMAIL_PATIENT_APP_URL", environment)
	if err != nil {
		return EmailConfig{}, err
	}
	professionalURL, err := applicationURLFromEnv("EMAIL_PROFESSIONAL_APP_URL", environment)
	if err != nil {
		return EmailConfig{}, err
	}
	timeout, err := durationFromEnv("EMAIL_TIMEOUT")
	if err != nil {
		return EmailConfig{}, err
	}
	workerEnabled, err := boolFromEnv("EMAIL_WORKER_ENABLED")
	if err != nil {
		return EmailConfig{}, err
	}
	if !workerEnabled && environment != "development" {
		return EmailConfig{}, fmt.Errorf("EMAIL_WORKER_ENABLED must be true outside development")
	}
	workerConcurrency, err := intFromEnv("EMAIL_WORKER_CONCURRENCY", 1, 16)
	if err != nil {
		return EmailConfig{}, err
	}
	workerPoll, err := durationFromEnv("EMAIL_WORKER_POLL_INTERVAL")
	if err != nil {
		return EmailConfig{}, err
	}
	workerLease, err := durationFromEnv("EMAIL_WORKER_LEASE")
	if err != nil {
		return EmailConfig{}, err
	}
	workerMaxAttempts, err := intFromEnv("EMAIL_WORKER_MAX_ATTEMPTS", 1, 10)
	if err != nil {
		return EmailConfig{}, err
	}

	apiKey := strings.TrimSpace(os.Getenv("EMAIL_BREVO_API_KEY"))
	if provider == "brevo" && apiKey == "" {
		return EmailConfig{}, fmt.Errorf("EMAIL_BREVO_API_KEY is required when EMAIL_PROVIDER=brevo")
	}
	return EmailConfig{
		Provider: provider, BrevoAPIKey: apiKey, FromName: fromName, FromAddress: fromAddress,
		PatientAppURL: patientURL, ProfessionalAppURL: professionalURL, Timeout: timeout,
		WorkerEnabled: workerEnabled, WorkerConcurrency: workerConcurrency,
		WorkerPoll: workerPoll, WorkerLease: workerLease, WorkerMaxAttempts: workerMaxAttempts,
	}, nil
}

func applicationURLFromEnv(key, environment string) (string, error) {
	value, err := stringFromEnv(key)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("%s must be an absolute HTTP(S) URL", key)
	}
	if environment != "development" && parsed.Scheme != "https" {
		return "", fmt.Errorf("%s must use HTTPS outside development", key)
	}
	return strings.TrimRight(value, "/"), nil
}

func loadCompanionConfig(environment string) (CompanionConfig, error) {
	enabled, err := boolFromEnv("COMPANION_ENABLED")
	if err != nil {
		return CompanionConfig{}, err
	}
	timeout, err := durationFromEnv("COMPANION_TIMEOUT")
	if err != nil {
		return CompanionConfig{}, err
	}
	historyMessages, err := intFromEnv("COMPANION_HISTORY_MESSAGES", 1, 50)
	if err != nil {
		return CompanionConfig{}, err
	}
	workerEnabled, err := boolFromEnv("AI_CONTEXT_WORKER_ENABLED")
	if err != nil {
		return CompanionConfig{}, err
	}
	workerConcurrency, err := intFromEnv("AI_CONTEXT_WORKER_CONCURRENCY", 1, 32)
	if err != nil {
		return CompanionConfig{}, err
	}
	workerPoll, err := durationFromEnv("AI_CONTEXT_WORKER_POLL_INTERVAL")
	if err != nil {
		return CompanionConfig{}, err
	}
	workerLease, err := durationFromEnv("AI_CONTEXT_WORKER_LEASE")
	if err != nil {
		return CompanionConfig{}, err
	}
	workerMaxAttempts, err := intFromEnv("AI_CONTEXT_WORKER_MAX_ATTEMPTS", 1, 10)
	if err != nil {
		return CompanionConfig{}, err
	}

	config := CompanionConfig{
		Enabled: enabled, Timeout: timeout, HistoryMessages: historyMessages,
		ContextWorkerEnabled: workerEnabled, ContextWorkerConcurrency: workerConcurrency,
		ContextWorkerPoll: workerPoll, ContextWorkerLease: workerLease,
		ContextWorkerMaxAttempts: workerMaxAttempts,
	}
	if !enabled {
		if workerEnabled {
			return CompanionConfig{}, fmt.Errorf("AI_CONTEXT_WORKER_ENABLED requires COMPANION_ENABLED")
		}
		return config, nil
	}

	config.BaseURL, err = stringFromEnv("COMPANION_BASE_URL")
	if err != nil {
		return CompanionConfig{}, err
	}
	config.APIKey, err = stringFromEnv("COMPANION_API_KEY")
	if err != nil {
		return CompanionConfig{}, err
	}
	if environment != "development" && !strings.HasPrefix(config.BaseURL, "https://") {
		return CompanionConfig{}, fmt.Errorf("COMPANION_BASE_URL must use HTTPS outside development")
	}

	return config, nil
}

func loadAuthConfig(environment string) (AuthConfig, error) {
	issuer, err := stringFromEnv("AUTH_ISSUER")
	if err != nil {
		return AuthConfig{}, err
	}

	jwtPrivateKey, err := base64BytesFromEnv("AUTH_JWT_PRIVATE_KEY_BASE64", 32)
	if err != nil {
		return AuthConfig{}, err
	}

	dataEncryptionKey, err := base64BytesFromEnv("AUTH_DATA_ENCRYPTION_KEY_BASE64", 32)
	if err != nil {
		return AuthConfig{}, err
	}

	accessTokenTTL, err := durationFromEnv("AUTH_ACCESS_TOKEN_TTL")
	if err != nil {
		return AuthConfig{}, err
	}

	refreshTokenTTL, err := durationFromEnv("AUTH_REFRESH_TOKEN_TTL")
	if err != nil {
		return AuthConfig{}, err
	}

	emailVerificationTokenTTL, err := durationFromEnv("AUTH_EMAIL_VERIFICATION_TOKEN_TTL")
	if err != nil {
		return AuthConfig{}, err
	}

	passwordResetTokenTTL, err := durationFromEnv("AUTH_PASSWORD_RESET_TOKEN_TTL")
	if err != nil {
		return AuthConfig{}, err
	}
	googleChallengeTTL, err := durationFromEnv("AUTH_GOOGLE_CHALLENGE_TTL")
	if err != nil {
		return AuthConfig{}, err
	}
	googleClientID := strings.TrimSpace(os.Getenv("AUTH_GOOGLE_CLIENT_ID"))
	if environment != "development" && googleClientID == "" {
		return AuthConfig{}, fmt.Errorf("AUTH_GOOGLE_CLIENT_ID is required outside development")
	}

	cookieSecure, err := boolFromEnv("AUTH_COOKIE_SECURE")
	if err != nil {
		return AuthConfig{}, err
	}

	allowedOrigins, err := stringSliceFromEnv("AUTH_ALLOWED_ORIGINS")
	if err != nil {
		return AuthConfig{}, err
	}

	exposeDevelopmentTokens, err := boolFromEnv("AUTH_EXPOSE_DEVELOPMENT_TOKENS")
	if err != nil {
		return AuthConfig{}, err
	}

	if exposeDevelopmentTokens && environment != "development" {
		return AuthConfig{}, fmt.Errorf(
			"AUTH_EXPOSE_DEVELOPMENT_TOKENS can only be true in development",
		)
	}

	webAuthnRPID, err := stringFromEnv("AUTH_WEBAUTHN_RP_ID")
	if err != nil {
		return AuthConfig{}, err
	}

	webAuthnRPDisplayName, err := stringFromEnv("AUTH_WEBAUTHN_RP_DISPLAY_NAME")
	if err != nil {
		return AuthConfig{}, err
	}

	webAuthnOrigins, err := stringSliceFromEnv("AUTH_WEBAUTHN_ORIGINS")
	if err != nil {
		return AuthConfig{}, err
	}

	webAuthnCeremonyTTL, err := durationFromEnv("AUTH_WEBAUTHN_CEREMONY_TTL")
	if err != nil {
		return AuthConfig{}, err
	}

	if environment != "development" {
		if !cookieSecure {
			return AuthConfig{}, fmt.Errorf("AUTH_COOKIE_SECURE must be true outside development")
		}
		if err := requireHTTPSOrigins("AUTH_ALLOWED_ORIGINS", allowedOrigins); err != nil {
			return AuthConfig{}, err
		}
		if err := requireHTTPSOrigins("AUTH_WEBAUTHN_ORIGINS", webAuthnOrigins); err != nil {
			return AuthConfig{}, err
		}
	}

	return AuthConfig{
		Issuer:                    issuer,
		JWTPrivateKey:             jwtPrivateKey,
		DataEncryptionKey:         dataEncryptionKey,
		AccessTokenTTL:            accessTokenTTL,
		RefreshTokenTTL:           refreshTokenTTL,
		EmailVerificationTokenTTL: emailVerificationTokenTTL,
		PasswordResetTokenTTL:     passwordResetTokenTTL,
		GoogleClientID:            googleClientID,
		GoogleChallengeTTL:        googleChallengeTTL,
		CookieSecure:              cookieSecure,
		AllowedOrigins:            allowedOrigins,
		ExposeDevelopmentTokens:   exposeDevelopmentTokens,
		WebAuthnRPID:              webAuthnRPID,
		WebAuthnRPDisplayName:     webAuthnRPDisplayName,
		WebAuthnOrigins:           webAuthnOrigins,
		WebAuthnCeremonyTTL:       webAuthnCeremonyTTL,
	}, nil
}

func requireHTTPSOrigins(key string, origins []string) error {
	for _, origin := range origins {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" ||
			parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("%s must contain only exact HTTPS origins", key)
		}
	}
	return nil
}

func stringFromEnv(key string) (string, error) {
	value, exists := os.LookupEnv(key)
	if !exists || value == "" {
		return "", fmt.Errorf("%s is required", key)
	}

	return value, nil
}

func durationFromEnv(key string) (time.Duration, error) {
	value, exists := os.LookupEnv(key)
	if !exists || value == "" {
		return 0, fmt.Errorf("%s is required", key)
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", key)
	}
	return duration, nil
}

func boolFromEnv(key string) (bool, error) {
	value, err := stringFromEnv(key)
	if err != nil {
		return false, err
	}

	switch strings.ToLower(value) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be true or false", key)
	}
}

func intFromEnv(key string, minimum, maximum int) (int, error) {
	value, err := stringFromEnv(key)
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	if parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("%s must be between %d and %d", key, minimum, maximum)
	}
	return parsed, nil
}

func stringSliceFromEnv(key string) ([]string, error) {
	value, err := stringFromEnv(key)
	if err != nil {
		return nil, err
	}

	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			return nil, fmt.Errorf("%s contains an empty value", key)
		}
		values = append(values, trimmed)
	}

	return values, nil
}

func base64BytesFromEnv(key string, expectedLength int) ([]byte, error) {
	value, err := stringFromEnv(key)
	if err != nil {
		return nil, err
	}

	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", key, err)
	}

	if len(decoded) != expectedLength {
		return nil, fmt.Errorf(
			"%s must decode to exactly %d bytes",
			key,
			expectedLength,
		)
	}

	return decoded, nil
}
