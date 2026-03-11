package notify_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/laenen-partners/notify"
	"github.com/laenen-partners/notify/email"
)

var testSeq = func() *atomic.Int64 {
	v := &atomic.Int64{}
	v.Store(time.Now().UnixNano() % 1_000_000)
	return v
}()

func uniqueAddr(prefix string) string {
	return fmt.Sprintf("notify-%s-%d@example.com", prefix, testSeq.Add(1))
}

// testDispatcher records dispatched events.
type testDispatcher struct {
	mu     sync.Mutex
	events []notify.Event
}

func (d *testDispatcher) Dispatch(_ context.Context, event notify.Event) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.events = append(d.events, event)
	return nil
}

func (d *testDispatcher) Events() []notify.Event {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]notify.Event{}, d.events...)
}

func TestNotify_PublishesScopesToNATS(t *testing.T) {
	nc := connectNATS(t)
	client := notify.New(nc)

	// Subscribe to the expected NATS subjects.
	var mu sync.Mutex
	var received []string

	sub1, err := nc.Subscribe("dsx.scope.family.members", func(msg *nats.Msg) {
		mu.Lock()
		received = append(received, msg.Subject)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub1.Unsubscribe()

	sub2, err := nc.Subscribe("dsx.scope.document.abc123", func(msg *nats.Msg) {
		mu.Lock()
		received = append(received, msg.Subject)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub2.Unsubscribe()

	nc.Flush()

	err = client.Notify(context.Background(), notify.Event{
		Scopes: []string{"family:members", "document:abc123"},
	})
	if err != nil {
		t.Fatal(err)
	}

	nc.Flush()
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(received))
	}
	// Messages may arrive in any order.
	got := map[string]bool{received[0]: true, received[1]: true}
	if !got["dsx.scope.family.members"] {
		t.Error("missing dsx.scope.family.members")
	}
	if !got["dsx.scope.document.abc123"] {
		t.Error("missing dsx.scope.document.abc123")
	}
}

func TestNotify_NilPayload(t *testing.T) {
	nc := connectNATS(t)
	client := notify.New(nc)

	var payload []byte
	var payloadSet bool

	sub, err := nc.Subscribe("dsx.scope.family.members", func(msg *nats.Msg) {
		payload = msg.Data
		payloadSet = true
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe()

	nc.Flush()

	err = client.Notify(context.Background(), notify.Event{
		Scopes: []string{"family:members"},
	})
	if err != nil {
		t.Fatal(err)
	}

	nc.Flush()
	time.Sleep(50 * time.Millisecond)

	if !payloadSet {
		t.Fatal("expected message to be received")
	}
	if len(payload) != 0 {
		t.Errorf("expected nil/empty payload, got %d bytes", len(payload))
	}
}

func TestNotify_StreamOnlySkipsDispatcher(t *testing.T) {
	nc := connectNATS(t)
	d := &testDispatcher{}
	client := notify.New(nc, notify.WithDispatcher(d))

	// No Kind — stream-only, dispatcher should not be called.
	err := client.Notify(context.Background(), notify.Event{
		Scopes: []string{"family:members"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(d.Events()) != 0 {
		t.Errorf("expected no dispatcher calls, got %d", len(d.Events()))
	}
}

func TestNotify_WithKindCallsDispatcher(t *testing.T) {
	nc := connectNATS(t)
	d := &testDispatcher{}
	client := notify.New(nc, notify.WithDispatcher(d))

	err := client.Notify(context.Background(), notify.Event{
		Scopes:    []string{"family:members"},
		Kind:      "member.added",
		ActorID:   "user-123",
		FamilyID:  "fam-456",
		EntityIDs: []string{"member-789"},
	})
	if err != nil {
		t.Fatal(err)
	}

	events := d.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 dispatcher call, got %d", len(events))
	}
	if events[0].Kind != "member.added" {
		t.Errorf("expected kind member.added, got %s", events[0].Kind)
	}
	if events[0].ActorID != "user-123" {
		t.Errorf("expected actor user-123, got %s", events[0].ActorID)
	}
}

func TestNotify_NoDispatcherWithKindIsNoop(t *testing.T) {
	nc := connectNATS(t)
	client := notify.New(nc) // no dispatcher

	// Should not panic or error — just publishes scopes and skips dispatch.
	err := client.Notify(context.Background(), notify.Event{
		Scopes: []string{"family:members"},
		Kind:   "member.added",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestNotify_CustomPrefix(t *testing.T) {
	nc := connectNATS(t)
	client := notify.New(nc, notify.WithSubjectPrefix("myapp.events"))

	var received string
	sub, err := nc.Subscribe("myapp.events.family.members", func(msg *nats.Msg) {
		received = msg.Subject
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe()

	nc.Flush()

	err = client.Notify(context.Background(), notify.Event{
		Scopes: []string{"family:members"},
	})
	if err != nil {
		t.Fatal(err)
	}

	nc.Flush()
	time.Sleep(50 * time.Millisecond)

	if received != "myapp.events.family.members" {
		t.Errorf("expected myapp.events.family.members, got %s", received)
	}
}

func TestNotify_EmptyScopes(t *testing.T) {
	nc := connectNATS(t)
	d := &testDispatcher{}
	client := notify.New(nc, notify.WithDispatcher(d))

	// Only Kind, no scopes — should still dispatch to notification service.
	err := client.Notify(context.Background(), notify.Event{
		Kind:     "member.added",
		ActorID:  "user-123",
		FamilyID: "fam-456",
	})
	if err != nil {
		t.Fatal(err)
	}

	events := d.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 dispatcher call, got %d", len(events))
	}
}

func TestNotify_SendsEmailViaSMTP(t *testing.T) {
	addr := uniqueAddr("invite")
	nc := connectNATS(t)
	sender := email.NewSMTPSender(email.SMTPConfig{
		Host: "localhost",
		Port: 1025,
		From: "butler@example.com",
	})
	client := notify.New(nc, notify.WithEmail(sender))

	err := client.Notify(context.Background(), notify.Event{
		Scopes:   []string{"family:invitations"},
		Kind:     "invitation.sent",
		ActorID:  "user-123",
		FamilyID: "fam-456",
		Emails: []email.Message{
			{
				To:      addr,
				Subject: "Join the Smith family on Butler",
				HTML:    "<h1>You're invited!</h1><p>Click here to join.</p>",
			},
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
	if msgs[0].To[0].Address != addr {
		t.Errorf("expected to=%s, got %s", addr, msgs[0].To[0].Address)
	}
	if msgs[0].Subject != "Join the Smith family on Butler" {
		t.Errorf("expected subject, got %s", msgs[0].Subject)
	}
}

func TestNotify_NoEmailSenderSkipsEmails(t *testing.T) {
	nc := connectNATS(t)
	client := notify.New(nc) // no email sender

	// Should not panic or error — just publishes scopes.
	err := client.Notify(context.Background(), notify.Event{
		Scopes: []string{"family:invitations"},
		Emails: []email.Message{
			{To: "someone@example.com", Subject: "test", Text: "hello"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestNotify_ScopesAndEmailTogether(t *testing.T) {
	bothAddr := uniqueAddr("both")
	nc := connectNATS(t)
	sender := email.NewSMTPSender(email.SMTPConfig{
		Host: "localhost",
		Port: 1025,
		From: "butler@example.com",
	})
	client := notify.New(nc, notify.WithEmail(sender))

	// Subscribe to NATS to verify scope was published.
	var scopeReceived bool
	sub, err := nc.Subscribe("dsx.scope.family.invitations", func(msg *nats.Msg) {
		scopeReceived = true
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe()
	nc.Flush()

	err = client.Notify(context.Background(), notify.Event{
		Scopes: []string{"family:invitations"},
		Kind:   "invitation.sent",
		Emails: []email.Message{
			{To: bothAddr, Subject: "Both channels", HTML: "<p>Hello</p>"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	nc.Flush()
	time.Sleep(200 * time.Millisecond)

	// Both channels should have fired.
	if !scopeReceived {
		t.Error("expected NATS scope to be published")
	}
	msgs := findMailpitMessages(t, bothAddr)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 email, got %d", len(msgs))
	}
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

func connectNATS(t *testing.T) *nats.Conn {
	t.Helper()
	url := natsURL(t)
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect to NATS at %s: %v", url, err)
	}
	t.Cleanup(nc.Close)
	return nc
}

func natsURL(t *testing.T) string {
	t.Helper()
	// Default to localhost for docker-compose NATS.
	return "nats://localhost:4222"
}
