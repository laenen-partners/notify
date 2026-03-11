package email_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/laenen-partners/notify/email"
)

var testSeq atomic.Int64

func uniqueAddr(prefix string) string {
	return fmt.Sprintf("%s-%d@example.com", prefix, testSeq.Add(1))
}

func TestSMTPSender_SendHTMLEmail(t *testing.T) {
	addr := uniqueAddr("html")
	sender := newTestSender()

	err := sender.Send(email.Message{
		To:      addr,
		Subject: "HTML email test",
		HTML:    "<h1>Welcome!</h1><p>Click <a href=\"https://example.com/invite/abc\">here</a> to join.</p>",
	})
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)
	msgs := findMailpitMessages(t, addr)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message to %s, got %d", addr, len(msgs))
	}
	if msgs[0].Subject != "HTML email test" {
		t.Errorf("expected subject, got %s", msgs[0].Subject)
	}
}

func TestSMTPSender_SendPlainTextEmail(t *testing.T) {
	addr := uniqueAddr("plain")
	sender := newTestSender()

	err := sender.Send(email.Message{
		To:      addr,
		Subject: "Plain text test",
		Text:    "This is plain text.",
	})
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)
	msgs := findMailpitMessages(t, addr)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message to %s, got %d", addr, len(msgs))
	}
}

func TestSMTPSender_SendMultipartEmail(t *testing.T) {
	addr := uniqueAddr("multi")
	sender := newTestSender()

	err := sender.Send(email.Message{
		To:      addr,
		Subject: "Multipart test",
		Text:    "Plain text version",
		HTML:    "<p>HTML version</p>",
	})
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)
	msgs := findMailpitMessages(t, addr)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message to %s, got %d", addr, len(msgs))
	}
}

func TestSMTPSender_MultipleRecipients(t *testing.T) {
	sender := newTestSender()

	addrs := []string{uniqueAddr("batch-a"), uniqueAddr("batch-b"), uniqueAddr("batch-c")}
	for _, addr := range addrs {
		err := sender.Send(email.Message{
			To:      addr,
			Subject: "Batch test",
			Text:    "Hello " + addr,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	time.Sleep(100 * time.Millisecond)
	for _, addr := range addrs {
		msgs := findMailpitMessages(t, addr)
		if len(msgs) != 1 {
			t.Errorf("expected 1 message to %s, got %d", addr, len(msgs))
		}
	}
}

func newTestSender() *email.SMTPSender {
	return email.NewSMTPSender(email.SMTPConfig{
		Host: "localhost",
		Port: 1025,
		From: "butler@example.com",
	})
}

// Mailpit API helpers.

type mailpitMessage struct {
	Subject string           `json:"Subject"`
	To      []mailpitAddress `json:"To"`
}

type mailpitAddress struct {
	Address string `json:"Address"`
}

type mailpitSearchResponse struct {
	Messages []mailpitMessage `json:"messages"`
}

func findMailpitMessages(t *testing.T, toAddress string) []mailpitMessage {
	t.Helper()
	resp, err := http.Get("http://localhost:8025/api/v1/search?query=to:" + toAddress)
	if err != nil {
		t.Fatalf("mailpit search API: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read mailpit response: %v", err)
	}

	var result mailpitSearchResponse
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("parse mailpit response: %v (body: %s)", err, body)
	}
	return result.Messages
}
