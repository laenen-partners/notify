// Package email provides an SMTP-based email sender for the notify library.
package email

import (
	"fmt"
	"net/smtp"
	"strings"
)

// Message represents an email to send.
type Message struct {
	To      string // recipient address
	Subject string
	HTML    string // HTML body
	Text    string // optional plain-text alternative
}

// Sender sends email messages.
type Sender interface {
	Send(msg Message) error
}

// SMTPConfig configures the SMTP sender.
type SMTPConfig struct {
	Host     string // SMTP host (e.g. "localhost")
	Port     int    // SMTP port (e.g. 1025 for Mailpit, 587 for production)
	From     string // sender address (e.g. "butler@example.com")
	Username string // optional SMTP auth username
	Password string // optional SMTP auth password
}

// SMTPSender sends emails via SMTP.
type SMTPSender struct {
	cfg SMTPConfig
}

// NewSMTPSender creates an SMTP email sender.
func NewSMTPSender(cfg SMTPConfig) *SMTPSender {
	return &SMTPSender{cfg: cfg}
}

// Send delivers an email via SMTP.
func (s *SMTPSender) Send(msg Message) error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)

	// Build MIME message.
	var body strings.Builder
	body.WriteString("From: " + s.cfg.From + "\r\n")
	body.WriteString("To: " + msg.To + "\r\n")
	body.WriteString("Subject: " + msg.Subject + "\r\n")
	body.WriteString("MIME-Version: 1.0\r\n")

	if msg.Text != "" && msg.HTML != "" {
		// Multipart alternative: plain text + HTML.
		boundary := "==notify-boundary=="
		body.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n")
		body.WriteString("\r\n")

		body.WriteString("--" + boundary + "\r\n")
		body.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
		body.WriteString("\r\n")
		body.WriteString(msg.Text + "\r\n")

		body.WriteString("--" + boundary + "\r\n")
		body.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n")
		body.WriteString("\r\n")
		body.WriteString(msg.HTML + "\r\n")

		body.WriteString("--" + boundary + "--\r\n")
	} else if msg.HTML != "" {
		body.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n")
		body.WriteString("\r\n")
		body.WriteString(msg.HTML)
	} else {
		body.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
		body.WriteString("\r\n")
		body.WriteString(msg.Text)
	}

	// Auth is optional (Mailpit doesn't need it).
	var auth smtp.Auth
	if s.cfg.Username != "" {
		auth = smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
	}

	return smtp.SendMail(addr, auth, s.cfg.From, []string{msg.To}, []byte(body.String()))
}
