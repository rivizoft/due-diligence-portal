package service

import (
	"io"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEmailService_Disabled(t *testing.T) {
	svc := &EmailService{enabled: false}
	assert.False(t, svc.IsEnabled())

	// Should not error when disabled — just logs.
	err := svc.SendInvite("test@test.com", "token123", "https://portal.example.com")
	assert.NoError(t, err)
}

func TestEmailService_EnabledButNoHost(t *testing.T) {
	svc := &EmailService{enabled: true, host: ""}
	assert.False(t, svc.IsEnabled())
}

func TestEmailService_EnabledWithHost(t *testing.T) {
	svc := &EmailService{enabled: true, host: "smtp.example.com"}
	assert.True(t, svc.IsEnabled())
}

func TestEmailService_SendInvite_Disabled(t *testing.T) {
	svc := &EmailService{enabled: false}
	err := svc.SendInvite("investor@example.com", "abc123", "https://portal.example.com")
	assert.NoError(t, err)
}

func TestEmailService_SendQANotification_Disabled(t *testing.T) {
	svc := &EmailService{enabled: false}
	err := svc.SendQANotification("admin@example.com", "Re: Financials", "Q3 Revenue", "Investor", "Can you explain?")
	assert.NoError(t, err)
}

func TestEmailService_SendDocumentNotification_Disabled(t *testing.T) {
	svc := &EmailService{enabled: false}
	err := svc.SendDocumentNotification("investor@example.com", "Q3 Financials.pdf", "Financials", "Admin")
	assert.NoError(t, err)
}

func TestEmailService_SendNDASignedNotification_Disabled(t *testing.T) {
	svc := &EmailService{enabled: false}
	err := svc.SendNDASignedNotification("admin@example.com", "John Smith", "john@vc.com", "Standard NDA")
	assert.NoError(t, err)
}

func TestEmailMessage_UnicodeSubject(t *testing.T) {
	message := buildEmailMessage("portal@example.com", "user@example.com", "Приглашение / Invitation", "Русский текст\nEnglish text")
	parsed, err := mail.ReadMessage(strings.NewReader(string(message)))
	if !assert.NoError(t, err) {
		return
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
	assert.NoError(t, err)
	assert.Equal(t, "Приглашение / Invitation", subject)
	assert.Contains(t, parsed.Header.Get("Subject"), "=?")
	assert.Contains(t, string(message), "charset=UTF-8")
}

func TestEmailMessage_UnicodeBody(t *testing.T) {
	body := "Русский текст\r\nEnglish text"
	message := buildEmailMessage("portal@example.com", "user@example.com", "Invitation", body)
	parsed, err := mail.ReadMessage(strings.NewReader(string(message)))
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, "quoted-printable", parsed.Header.Get("Content-Transfer-Encoding"))
	decoded, err := io.ReadAll(quotedprintable.NewReader(parsed.Body))
	assert.NoError(t, err)
	assert.Equal(t, body, string(decoded))
	for _, value := range message {
		assert.Less(t, value, byte(128), "SMTP message must use only ASCII transport bytes")
	}
}
