package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/Vini-create/psycho-app-back/internal/auth"
	"github.com/Vini-create/psycho-app-back/internal/care"
	"github.com/Vini-create/psycho-app-back/internal/chat"
	"github.com/Vini-create/psycho-app-back/internal/companion"
	"github.com/Vini-create/psycho-app-back/internal/config"
	appemail "github.com/Vini-create/psycho-app-back/internal/email"
	"github.com/Vini-create/psycho-app-back/internal/httpapi"
	"github.com/Vini-create/psycho-app-back/internal/insight"
	"github.com/Vini-create/psycho-app-back/internal/platform/postgres"
)

func main() {
	if err := run(); err != nil {
		slog.Error("failed to run server", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	signalCtx, stopSignals := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stopSignals()

	databaseCtx, cancelDatabase := context.WithTimeout(
		signalCtx,
		cfg.Database.ConnectTimeout,
	)

	databasePool, err := postgres.Open(
		databaseCtx,
		cfg.Database.URL,
	)

	cancelDatabase()

	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	defer databasePool.Close()

	slog.Info("database connection established")

	accessTokenManager, err := auth.NewAccessTokenManager(
		cfg.Auth.Issuer,
		cfg.Auth.JWTPrivateKey,
		cfg.Auth.AccessTokenTTL,
	)
	if err != nil {
		return fmt.Errorf("create access token manager: %w", err)
	}

	secretCipher, err := auth.NewSecretCipher(cfg.Auth.DataEncryptionKey)
	if err != nil {
		return fmt.Errorf("create auth secret cipher: %w", err)
	}
	var googleVerifier auth.GoogleTokenVerifier = auth.DisabledGoogleVerifier{}
	if cfg.Auth.GoogleClientID != "" {
		googleVerifier, err = auth.NewGoogleIDTokenVerifier(cfg.Auth.GoogleClientID)
		if err != nil {
			return fmt.Errorf("create Google token verifier: %w", err)
		}
	}

	authRepository := auth.NewRepository(databasePool)
	passkeyManager, err := auth.NewPasskeyManager(
		authRepository,
		secretCipher,
		auth.PasskeyConfig{
			RPID:          cfg.Auth.WebAuthnRPID,
			RPDisplayName: cfg.Auth.WebAuthnRPDisplayName,
			RPOrigins:     cfg.Auth.WebAuthnOrigins,
			CeremonyTTL:   cfg.Auth.WebAuthnCeremonyTTL,
		},
	)
	if err != nil {
		return fmt.Errorf("create passkey manager: %w", err)
	}

	authService, err := auth.NewService(
		authRepository,
		accessTokenManager,
		passkeyManager,
		secretCipher,
		googleVerifier,
		auth.ServiceConfig{
			RefreshTokenTTL:           cfg.Auth.RefreshTokenTTL,
			EmailVerificationTokenTTL: cfg.Auth.EmailVerificationTokenTTL,
			PasswordResetTokenTTL:     cfg.Auth.PasswordResetTokenTTL,
			GoogleChallengeTTL:        cfg.Auth.GoogleChallengeTTL,
			ExposeDevelopmentTokens:   cfg.Auth.ExposeDevelopmentTokens,
		},
	)
	if err != nil {
		return fmt.Errorf("create auth service: %w", err)
	}

	authHandler := httpapi.NewAuthHandler(
		authService,
		httpapi.AuthHandlerConfig{
			CookieSecure:    cfg.Auth.CookieSecure,
			RefreshTokenTTL: cfg.Auth.RefreshTokenTTL,
			AllowedOrigins:  cfg.Auth.AllowedOrigins,
		},
	)

	var companionClient companion.Client = companion.UnavailableClient{}
	if cfg.Companion.Enabled {
		httpCompanionClient, err := companion.NewHTTPClient(
			cfg.Companion.BaseURL,
			cfg.Companion.APIKey,
			cfg.Companion.Timeout,
		)
		if err != nil {
			return fmt.Errorf("create companion client: %w", err)
		}
		companionClient = httpCompanionClient
	}

	chatRepository := chat.NewRepository(databasePool)
	chatService, err := chat.NewService(
		chatRepository,
		secretCipher,
		companionClient,
		chat.ServiceConfig{
			ConsentPolicyVersion: cfg.App.ConsentPolicyVersion,
			HistoryMessages:      cfg.Companion.HistoryMessages,
		},
	)
	if err != nil {
		return fmt.Errorf("create chat service: %w", err)
	}
	chatHandler := httpapi.NewChatHandler(chatService)
	careRepository := care.NewRepository(databasePool)
	careService, err := care.NewService(
		careRepository,
		care.ServiceConfig{
			InvitationTTL:        cfg.App.InvitationTTL,
			ConsentPolicyVersion: cfg.App.ConsentPolicyVersion,
			PatientAppURL:        cfg.Email.PatientAppURL,
		},
	)
	if err != nil {
		return fmt.Errorf("create care service: %w", err)
	}
	careHandler := httpapi.NewCareHandler(careService)
	insightRepository := insight.NewRepository(databasePool)
	insightService, err := insight.NewService(
		insightRepository,
		secretCipher,
		companionClient,
		insight.ServiceConfig{
			ConsentPolicyVersion: cfg.App.ConsentPolicyVersion,
			WorkerLease:          cfg.Companion.ContextWorkerLease,
			MaxAttempts:          cfg.Companion.ContextWorkerMaxAttempts,
		},
	)
	if err != nil {
		return fmt.Errorf("create insight service: %w", err)
	}
	insightHandler := httpapi.NewInsightHandler(insightService)

	var emailSender appemail.Sender = appemail.MockSender{}
	if cfg.Email.Provider == "brevo" {
		brevoSender, err := appemail.NewBrevoSender(
			cfg.Email.BrevoAPIKey,
			cfg.Email.FromName,
			cfg.Email.FromAddress,
			cfg.Email.Timeout,
		)
		if err != nil {
			return fmt.Errorf("create Brevo sender: %w", err)
		}
		emailSender = brevoSender
	}
	emailService, err := appemail.NewService(
		appemail.NewRepository(databasePool),
		emailSender,
		secretCipher,
		appemail.ServiceConfig{
			PatientAppURL:      cfg.Email.PatientAppURL,
			ProfessionalAppURL: cfg.Email.ProfessionalAppURL,
			Lease:              cfg.Email.WorkerLease,
			MaxAttempts:        cfg.Email.WorkerMaxAttempts,
		},
	)
	if err != nil {
		return fmt.Errorf("create email service: %w", err)
	}
	var emailWorkerDone chan struct{}
	if cfg.Email.WorkerEnabled {
		emailWorker, err := appemail.NewWorker(emailService, appemail.WorkerConfig{
			Concurrency:  cfg.Email.WorkerConcurrency,
			PollInterval: cfg.Email.WorkerPoll,
		})
		if err != nil {
			return fmt.Errorf("create email worker: %w", err)
		}
		emailWorkerDone = make(chan struct{})
		go func() {
			defer close(emailWorkerDone)
			emailWorker.Run(signalCtx)
		}()
		slog.Info(
			"email workers started",
			"provider", cfg.Email.Provider,
			"concurrency", cfg.Email.WorkerConcurrency,
		)
	}

	var contextWorkerDone chan struct{}
	if cfg.Companion.ContextWorkerEnabled {
		contextWorker, err := insight.NewWorker(insightService, insight.WorkerConfig{
			Concurrency:  cfg.Companion.ContextWorkerConcurrency,
			PollInterval: cfg.Companion.ContextWorkerPoll,
		})
		if err != nil {
			return fmt.Errorf("create insight worker: %w", err)
		}
		contextWorkerDone = make(chan struct{})
		go func() {
			defer close(contextWorkerDone)
			contextWorker.Run(signalCtx)
		}()
		slog.Info(
			"context workers started",
			"concurrency", cfg.Companion.ContextWorkerConcurrency,
		)
	}

	router := httpapi.NewRouter(
		authHandler, chatHandler, careHandler, insightHandler, cfg.Auth.AllowedOrigins,
	)

	server := http.Server{
		Addr:              cfg.HTTP.Address,
		Handler:           router,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
		MaxHeaderBytes:    cfg.HTTP.MaxHeaderBytes,
	}

	serverErrors := make(chan error, 1)

	slog.Info("starting HTTP server", "address", server.Addr)

	go func() {
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("listen and serve: %w", err)
		}

		return nil

	case <-signalCtx.Done():
		slog.Info("shutdown signal received")
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(
		context.Background(),
		cfg.HTTP.ShutdownTimeout,
	)
	defer cancelShutdown()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}

	serverErr := <-serverErrors

	if serverErr != nil && !errors.Is(serverErr, http.ErrServerClosed) {
		return fmt.Errorf("HTTP server stopped during shutdown: %w", serverErr)
	}
	if contextWorkerDone != nil {
		select {
		case <-contextWorkerDone:
		case <-shutdownCtx.Done():
			return fmt.Errorf("wait for context workers: %w", shutdownCtx.Err())
		}
	}
	if emailWorkerDone != nil {
		select {
		case <-emailWorkerDone:
		case <-shutdownCtx.Done():
			return fmt.Errorf("wait for email workers: %w", shutdownCtx.Err())
		}
	}

	slog.Info("HTTP server stopped")

	return nil
}
