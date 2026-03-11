package notify_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/laenen-partners/notify"
)

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
	if received[0] != "dsx.scope.family.members" {
		t.Errorf("expected dsx.scope.family.members, got %s", received[0])
	}
	if received[1] != "dsx.scope.document.abc123" {
		t.Errorf("expected dsx.scope.document.abc123, got %s", received[1])
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
