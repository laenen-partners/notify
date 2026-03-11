// Package notify provides a lightweight event dispatcher for domain events.
//
// Stream scope invalidations are published directly to NATS (fast path, no RPC).
// Emails are sent directly via SMTP. Other notification channels (SMS, push) are
// forwarded to a notification service via a pluggable Dispatcher interface.
package notify

import (
	"context"
	"fmt"
	"strings"

	"github.com/nats-io/nats.go"

	"github.com/laenen-partners/notify/email"
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
	// When empty, the event is stream-only (no dispatch to notification service).
	Kind string

	// ActorID identifies who or what triggered the event.
	// A member ID for user actions, "system" for workflows/jobs.
	ActorID string

	// FamilyID scopes the event to a family.
	FamilyID string

	// EntityIDs lists the entities involved in the event.
	EntityIDs []string

	// Emails to send as part of this event.
	// Sent directly via SMTP — no RPC hop.
	Emails []email.Message
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

// WithDispatcher sets a Dispatcher for non-stream channels (SMS, push).
func WithDispatcher(d Dispatcher) Option {
	return func(c *Client) { c.dispatcher = d }
}

// WithEmail sets an email sender for direct SMTP delivery.
func WithEmail(s email.Sender) Option {
	return func(c *Client) { c.email = s }
}

// Client dispatches domain events. Stream scopes go directly to NATS.
// Emails go directly via SMTP. Non-stream events are forwarded to an
// optional Dispatcher.
type Client struct {
	nc         *nats.Conn
	email      email.Sender
	dispatcher Dispatcher
	prefix     string
}

// New creates a notify client.
//
//	nc   — NATS connection for direct stream scope publishing.
//	opts — optional configuration (email sender, dispatcher, subject prefix).
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
// Emails are sent directly via SMTP when an email sender is configured.
// If the event has a Kind and a Dispatcher is configured, it is also forwarded
// for potential SMS/push delivery.
func (c *Client) Notify(ctx context.Context, event Event) error {
	// Fast path: publish scope invalidations directly to NATS.
	for _, scope := range event.Scopes {
		topic := c.prefix + "." + scopeToSubject(scope)
		if err := c.nc.Publish(topic, nil); err != nil {
			return fmt.Errorf("notify: publish scope %q: %w", scope, err)
		}
	}

	// Send emails directly via SMTP.
	if c.email != nil {
		for _, msg := range event.Emails {
			if err := c.email.Send(msg); err != nil {
				return fmt.Errorf("notify: send email to %q: %w", msg.To, err)
			}
		}
	}

	// Forward to notification service for other channels.
	if c.dispatcher != nil && event.Kind != "" {
		if err := c.dispatcher.Dispatch(ctx, event); err != nil {
			return fmt.Errorf("notify: dispatch %q: %w", event.Kind, err)
		}
	}

	return nil
}

// scopeToSubject converts a colon-separated scope to a dot-separated NATS subject.
// "family:members" → "family.members"
func scopeToSubject(scope string) string {
	return strings.ReplaceAll(scope, ":", ".")
}
