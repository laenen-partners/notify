// Package notify provides a lightweight event dispatcher for domain events.
//
// Stream scope invalidations are published directly to NATS (fast path, no RPC).
// Other notification channels (email, SMS, push) are forwarded to a notification
// service via Connect-RPC (future — currently a no-op when no RPC client is configured).
package notify

import (
	"context"
	"strings"

	"github.com/nats-io/nats.go"
)

const defaultPrefix = "dsx.scope"

// Event describes a domain event. Producers create these after mutations.
type Event struct {
	// Scopes to invalidate in DSX streams.
	// Published directly to NATS — only scope IDs, nil payload.
	// Example: "family:members", "document:abc123"
	Scopes []string

	// Kind identifies the domain event type for routing to notification channels.
	// Example: "member.added", "document.processed", "invitation.sent"
	// When empty, the event is stream-only (no RPC to notification service).
	Kind string

	// ActorID identifies who or what triggered the event.
	// A member ID for user actions, "system" for workflows/jobs.
	ActorID string

	// FamilyID scopes the event to a family.
	FamilyID string

	// EntityIDs lists the entities involved in the event.
	EntityIDs []string
}

// Dispatcher forwards domain events to notification channels.
// Implement this to integrate with the notification Connect-RPC service.
type Dispatcher interface {
	Dispatch(ctx context.Context, event Event) error
}

// Option configures a Client.
type Option func(*Client)

// WithSubjectPrefix overrides the default NATS subject prefix ("dsx.scope").
func WithSubjectPrefix(prefix string) Option {
	return func(c *Client) { c.prefix = prefix }
}

// WithDispatcher sets a Dispatcher for non-stream channels (email, SMS, push).
func WithDispatcher(d Dispatcher) Option {
	return func(c *Client) { c.dispatcher = d }
}

// Client dispatches domain events. Stream scopes go directly to NATS.
// Non-stream events are forwarded to an optional Dispatcher.
type Client struct {
	nc         *nats.Conn
	dispatcher Dispatcher
	prefix     string
}

// New creates a notify client.
//
//	nc         — NATS connection for direct stream scope publishing.
//	opts       — optional configuration (dispatcher, subject prefix).
func New(nc *nats.Conn, opts ...Option) *Client {
	c := &Client{nc: nc, prefix: defaultPrefix}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Notify dispatches an event.
//
// Stream scopes are published directly to NATS as topic-only messages (nil payload).
// If the event has a Kind and a Dispatcher is configured, it is also forwarded
// for potential email/SMS/push delivery.
func (c *Client) Notify(ctx context.Context, event Event) error {
	// Fast path: publish scope invalidations directly to NATS.
	for _, scope := range event.Scopes {
		topic := c.prefix + "." + scopeToSubject(scope)
		if err := c.nc.Publish(topic, nil); err != nil {
			return err
		}
	}

	// Forward to notification service for other channels.
	if c.dispatcher != nil && event.Kind != "" {
		return c.dispatcher.Dispatch(ctx, event)
	}

	return nil
}

// scopeToSubject converts a colon-separated scope to a dot-separated NATS subject.
// "family:members" → "family.members"
func scopeToSubject(scope string) string {
	return strings.ReplaceAll(scope, ":", ".")
}
