// Package notify provides a lightweight event dispatcher for domain events.
//
// Stream scope invalidations are published directly to NATS (fast path, no RPC).
// Emails are sent via the notification service Connect-RPC API. Other channels
// (SMS, push) are forwarded via a pluggable Dispatcher interface.
package notify

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	"github.com/nats-io/nats.go"

	"github.com/laenen-partners/notify/email"
	notifyv1 "github.com/laenen-partners/notify/gen/notify/v1"
	"github.com/laenen-partners/notify/gen/notify/v1/notifyv1connect"
)

const defaultPrefix = "dsx.scope"

// Event describes a domain event. Producers create these after mutations.
type Event struct {
	// Scopes to invalidate in DSX streams.
	// Published directly to NATS — only scope IDs, nil payload.
	Scopes []string

	// Kind identifies the domain event type for routing to notification channels.
	// When empty, the event is stream-only (no dispatch to notification service).
	Kind string

	// ActorID identifies who or what triggered the event.
	ActorID string

	// FamilyID scopes the event to a family.
	FamilyID string

	// EntityIDs lists the entities involved in the event.
	EntityIDs []string

	// Emails to send via the notification service.
	Emails []email.Message
}

// Dispatcher forwards domain events to notification channels.
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

// WithNotificationService sets the Connect-RPC client for the notification
// service. When configured, emails are sent via the service's SendEmail RPC.
func WithNotificationService(client notifyv1connect.NotificationServiceClient) Option {
	return func(c *Client) { c.rpc = client }
}

// WithEmail sets a direct email sender (bypasses the notification service).
// Use this for simple setups where no notification service is running.
func WithEmail(s email.Sender) Option {
	return func(c *Client) { c.email = s }
}

// Client dispatches domain events. Stream scopes go directly to NATS.
// Emails go via the notification service Connect-RPC API (or direct SMTP as fallback).
type Client struct {
	nc         *nats.Conn
	rpc        notifyv1connect.NotificationServiceClient
	email      email.Sender
	dispatcher Dispatcher
	prefix     string
}

// New creates a notify client.
//
//	nc   — NATS connection for direct stream scope publishing.
//	opts — optional configuration (notification service, email sender, dispatcher).
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
// Emails are sent via the notification service RPC (preferred) or direct SMTP (fallback).
// If the event has a Kind and a Dispatcher is configured, it is also forwarded.
func (c *Client) Notify(ctx context.Context, event Event) error {
	// Fast path: publish scope invalidations directly to NATS.
	for _, scope := range event.Scopes {
		topic := c.prefix + "." + scopeToSubject(scope)
		if err := c.nc.Publish(topic, nil); err != nil {
			return fmt.Errorf("notify: publish scope %q: %w", scope, err)
		}
	}

	// Send emails via notification service RPC (preferred) or direct SMTP (fallback).
	for _, msg := range event.Emails {
		if err := c.sendEmail(ctx, msg); err != nil {
			return fmt.Errorf("notify: send email to %q: %w", msg.To, err)
		}
	}

	// Forward to dispatcher for other channels.
	if c.dispatcher != nil && event.Kind != "" {
		if err := c.dispatcher.Dispatch(ctx, event); err != nil {
			return fmt.Errorf("notify: dispatch %q: %w", event.Kind, err)
		}
	}

	return nil
}

// sendEmail sends an email via the notification service RPC or direct SMTP.
func (c *Client) sendEmail(ctx context.Context, msg email.Message) error {
	if c.rpc != nil {
		_, err := c.rpc.SendEmail(ctx, connect.NewRequest(&notifyv1.SendEmailRequest{
			To:      msg.To,
			Subject: msg.Subject,
			Html:    msg.HTML,
			Text:    msg.Text,
		}))
		return err
	}
	if c.email != nil {
		return c.email.Send(msg)
	}
	return nil
}

// scopeToSubject converts a colon-separated scope to a dot-separated NATS subject.
func scopeToSubject(scope string) string {
	return strings.ReplaceAll(scope, ":", ".")
}
