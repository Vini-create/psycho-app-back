package email

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type Cipher interface {
	Decrypt([]byte) ([]byte, error)
}

type ServiceConfig struct {
	PatientAppURL      string
	ProfessionalAppURL string
	Lease              time.Duration
	MaxAttempts        int
}

type Service struct {
	repository *Repository
	sender     Sender
	cipher     Cipher
	config     ServiceConfig
}

func NewService(repository *Repository, sender Sender, cipher Cipher, config ServiceConfig) (*Service, error) {
	if repository == nil || sender == nil || cipher == nil {
		return nil, fmt.Errorf("email service dependencies are required")
	}
	if config.Lease <= 0 || config.MaxAttempts < 1 {
		return nil, fmt.Errorf("email worker configuration is invalid")
	}
	config.PatientAppURL = strings.TrimRight(config.PatientAppURL, "/")
	config.ProfessionalAppURL = strings.TrimRight(config.ProfessionalAppURL, "/")
	if config.PatientAppURL == "" || config.ProfessionalAppURL == "" {
		return nil, fmt.Errorf("email application URLs are required")
	}
	return &Service{repository: repository, sender: sender, cipher: cipher, config: config}, nil
}

func (s *Service) ProcessNext(ctx context.Context) (bool, error) {
	stored, found, err := s.repository.Claim(ctx, s.config.Lease)
	if err != nil || !found {
		return found, err
	}
	tokenBytes, err := s.cipher.Decrypt(stored.TokenCiphertext)
	if err != nil {
		_ = s.repository.MarkFailed(ctx, stored.ID, stored.AttemptCount, s.config.MaxAttempts, "decrypt_error")
		return true, fmt.Errorf("decrypt email token: %w", err)
	}
	message, err := s.buildMessage(stored.Audience, stored.Kind, stored.Recipient, string(tokenBytes))
	if err != nil {
		_ = s.repository.MarkFailed(ctx, stored.ID, stored.AttemptCount, s.config.MaxAttempts, "template_error")
		return true, err
	}
	message.IdempotencyKey = stored.ID
	providerID, err := s.sender.Send(ctx, message)
	if err != nil {
		if markErr := s.repository.MarkFailed(
			ctx, stored.ID, stored.AttemptCount, s.config.MaxAttempts, deliveryErrorCode(err),
		); markErr != nil {
			return true, markErr
		}
		return true, err
	}
	if err := s.repository.MarkSent(ctx, stored.ID, providerID); err != nil {
		return true, err
	}
	return true, nil
}

func (s *Service) buildMessage(audience, kind, recipient, token string) (Message, error) {
	baseURL := s.config.PatientAppURL
	if audience == "professional" {
		baseURL = s.config.ProfessionalAppURL
	} else if audience != "app" {
		return Message{}, fmt.Errorf("unsupported email audience")
	}
	path := "/verificar-email"
	subject := "Confirme seu e-mail na Sinapsa"
	title := "Confirme seu e-mail"
	instruction := "Confirme seu e-mail para ativar sua conta na Sinapsa."
	action := "Confirmar e-mail"
	expiry := "Este link é válido por 24 horas. Se você não criou esta conta, ignore esta mensagem."
	tag := "email-verification"
	if kind == "password_reset" {
		path = "/redefinir-senha"
		subject = "Redefina sua senha da Sinapsa"
		title = "Redefina sua senha"
		instruction = "Recebemos uma solicitação para criar uma nova senha para sua conta."
		action = "Criar nova senha"
		expiry = "Este link é válido por 30 minutos. Se você não fez esta solicitação, ignore esta mensagem."
		tag = "password-reset"
	} else if kind != "email_verification" {
		return Message{}, fmt.Errorf("unsupported email kind")
	}
	link, err := url.Parse(baseURL + path)
	if err != nil {
		return Message{}, fmt.Errorf("build email URL: %w", err)
	}
	query := link.Query()
	query.Set("token", token)
	query.Set("email", recipient)
	link.RawQuery = query.Encode()
	// Password and verification links must bypass Brevo's HTML click-tracking
	// redirect. The branded redirect endpoint is external to this service and a
	// certificate fault there must never make an account recovery link unusable.
	content := "Sinapsa\n\n" + title + "\n\n" + instruction + "\n\n" + action + ":\n" + link.String() + "\n\n" + expiry
	return Message{To: recipient, Subject: subject, TextContent: content, Tags: []string{"sinapsa", tag, audience}}, nil
}
