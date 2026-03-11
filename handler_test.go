package notify_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/laenen-partners/notify"
	"github.com/laenen-partners/notify/email"
	notifyv1 "github.com/laenen-partners/notify/gen/notify/v1"
	"github.com/laenen-partners/notify/gen/notify/v1/notifyv1connect"
)

var handlerSeq = func() *atomic.Int64 {
	v := &atomic.Int64{}
	v.Store(time.Now().UnixNano() % 1_000_000)
	return v
}()

func handlerAddr(prefix string) string {
	return fmt.Sprintf("handler-%s-%d@example.com", prefix, handlerSeq.Add(1))
}

func TestHandler_SendEmail(t *testing.T) {
	addr := handlerAddr("rpc")
	sender := email.NewSMTPSender(email.SMTPConfig{
		Host: "localhost",
		Port: 1025,
		From: "butler@example.com",
	})

	_, handler := notifyv1connect.NewNotificationServiceHandler(notify.NewHandler(sender))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := notifyv1connect.NewNotificationServiceClient(http.DefaultClient, srv.URL)

	_, err := client.SendEmail(context.Background(), connect.NewRequest(&notifyv1.SendEmailRequest{
		To:      addr,
		Subject: "RPC email test",
		Html:    "<p>Hello from RPC</p>",
	}))
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)
	msgs := findMailpitMessages(t, addr)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 email, got %d", len(msgs))
	}
	if msgs[0].Subject != "RPC email test" {
		t.Errorf("expected subject 'RPC email test', got %s", msgs[0].Subject)
	}
}

func TestNotify_SendsEmailViaRPC(t *testing.T) {
	addr := handlerAddr("via-rpc")
	sender := email.NewSMTPSender(email.SMTPConfig{
		Host: "localhost",
		Port: 1025,
		From: "butler@example.com",
	})

	// Start a notification service.
	_, handler := notifyv1connect.NewNotificationServiceHandler(notify.NewHandler(sender))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// Create a notify client that uses the RPC service for emails.
	nc := connectNATS(t)
	rpcClient := notifyv1connect.NewNotificationServiceClient(http.DefaultClient, srv.URL)
	client := notify.New(nc, notify.WithNotificationService(rpcClient))

	err := client.Notify(context.Background(), notify.Event{
		Scopes: []string{"family:invitations"},
		Kind:   "invitation.sent",
		Emails: []email.Message{
			{To: addr, Subject: "Via RPC service", HTML: "<p>Sent through notification service</p>"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)
	msgs := findMailpitMessages(t, addr)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 email, got %d", len(msgs))
	}
	if msgs[0].Subject != "Via RPC service" {
		t.Errorf("expected subject 'Via RPC service', got %s", msgs[0].Subject)
	}
}

func TestHandler_SendEmailWithAuth(t *testing.T) {
	addr := handlerAddr("auth")
	sender := email.NewSMTPSender(email.SMTPConfig{
		Host: "localhost",
		Port: 1025,
		From: "butler@example.com",
	})

	apiKeys := []string{"test-key-123"}
	_, handler := notifyv1connect.NewNotificationServiceHandler(
		notify.NewHandler(sender),
		connect.WithInterceptors(notify.NewAuthInterceptor(apiKeys)),
	)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := notifyv1connect.NewNotificationServiceClient(http.DefaultClient, srv.URL)

	// Without auth — should fail.
	_, err := client.SendEmail(context.Background(), connect.NewRequest(&notifyv1.SendEmailRequest{
		To:      addr,
		Subject: "Should fail",
	}))
	if err == nil {
		t.Fatal("expected unauthenticated error")
	}
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Errorf("expected CodeUnauthenticated, got %v", connect.CodeOf(err))
	}

	// With valid auth — should succeed.
	req := connect.NewRequest(&notifyv1.SendEmailRequest{
		To:      addr,
		Subject: "Auth email test",
		Html:    "<p>Authenticated</p>",
	})
	req.Header().Set("Authorization", "Bearer test-key-123")

	_, err = client.SendEmail(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)
	msgs := findMailpitMessages(t, addr)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 email, got %d", len(msgs))
	}
}
