package email

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestBuildMessageUsesAudienceAndEscapesLink(t *testing.T) {
	t.Parallel()
	service := &Service{config: ServiceConfig{
		PatientAppURL: "https://app.example.com", ProfessionalAppURL: "https://pro.example.com",
		Lease: time.Minute, MaxAttempts: 3,
	}}
	message, err := service.buildMessage("professional", "password_reset", "person+tag@example.com", "token&value")
	if err != nil {
		t.Fatalf("buildMessage() error = %v", err)
	}
	if message.To != "person+tag@example.com" || !strings.Contains(message.Subject, "senha") {
		t.Fatalf("unexpected message: %+v", message)
	}
	if message.HTMLContent != "" || !strings.Contains(message.TextContent, "https://pro.example.com/redefinir-senha?") ||
		!strings.Contains(message.TextContent, url.QueryEscape("token&value")) {
		t.Fatalf("message does not contain a safe reset link: %+v", message)
	}
}

func TestBuildMessageRejectsUnknownTemplate(t *testing.T) {
	t.Parallel()
	service := &Service{config: ServiceConfig{PatientAppURL: "https://app.example.com"}}
	if _, err := service.buildMessage("app", "unknown", "person@example.com", "token"); err == nil {
		t.Fatal("buildMessage() expected an error")
	}
}
