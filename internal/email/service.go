package email

import (
	"context"
	"fmt"
	"html"
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
	escapedLink := html.EscapeString(link.String())

	content := `<!doctype html><html lang="pt-BR"><body style="margin:0;background:#f5f1e8;color:#25211d;font-family:Arial,sans-serif">` +
		`<div style="max-width:560px;margin:0 auto;padding:40px 24px">` +
		`<p style="font-size:13px;letter-spacing:.12em;text-transform:uppercase">Sinapsa</p>` +
		`<h1 style="font-family:Georgia,serif;font-size:32px;line-height:1.15">` + html.EscapeString(title) + `</h1>` +
		`<p style="font-size:17px;line-height:1.6">` + html.EscapeString(instruction) + `</p>` +
		`<p style="margin:28px 0"><a href="` + escapedLink + `" style="display:inline-block;padding:14px 22px;background:#25211d;color:#fff;text-decoration:none;border-radius:4px;font-weight:700">` + html.EscapeString(action) + `</a></p>` +
		`<p style="color:#625b52;font-size:13px;line-height:1.5">` + html.EscapeString(expiry) + `</p>` +
		`</div></body></html>`
	return Message{To: recipient, Subject: subject, HTMLContent: content, Tags: []string{"sinapsa", tag, audience}}, nil
}
