# notify

Lightweight domain event dispatcher for Go. Publishes stream scope invalidations directly to NATS and sends emails via a Connect-RPC notification service (or direct SMTP).

## Architecture

```
Producer (any Go service)
    │
    ▼
notify.Client
    ├── Scopes ──▶ NATS publish (direct, nil payload, sub-ms)
    ├── Emails ──▶ NotificationService RPC ──▶ SMTP
    └── Kind   ──▶ Dispatcher interface (future: SMS, push)
```

**Stream invalidations** go straight to NATS — no RPC hop, no payload. Only the scope ID is published (e.g. `dsx.scope.family.members`). Browsers subscribed via DSX streams detect the stale signal and re-fetch through authenticated requests.

**Emails** are sent through the notification service's `SendEmail` RPC, which delivers via SMTP. A direct SMTP fallback (`WithEmail`) is available for simple setups without a running notification service.

## Installation

```bash
go get github.com/laenen-partners/notify
```

## Client Usage

### Stream-only (scope invalidation)

```go
nc, _ := nats.Connect("nats://localhost:4222")
client := notify.New(nc)

client.Notify(ctx, notify.Event{
    Scopes: []string{"family:members"},
})
```

### With notification service (emails via RPC)

```go
nc, _ := nats.Connect("nats://localhost:4222")
rpcClient := notifyv1connect.NewNotificationServiceClient(
    http.DefaultClient,
    "http://localhost:3001",
)
client := notify.New(nc, notify.WithNotificationService(rpcClient))

client.Notify(ctx, notify.Event{
    Scopes:    []string{"family:invitations"},
    Kind:      "invitation.sent",
    ActorID:   claims.Subject,
    FamilyID:  family.ID,
    EntityIDs: []string{invitation.ID},
    Emails: []email.Message{
        {
            To:      "invitee@example.com",
            Subject: "You're invited to the Smith family",
            HTML:    "<h1>Welcome!</h1><p>Click here to join.</p>",
        },
    },
})
```

### Direct SMTP (no notification service)

```go
sender := email.NewSMTPSender(email.SMTPConfig{
    Host: "localhost",
    Port: 1025,
    From: "butler@example.com",
})
client := notify.New(nc, notify.WithEmail(sender))
```

## Event Fields

| Field | Purpose |
|---|---|
| `Scopes` | DSX stream scopes to invalidate via NATS (e.g. `"family:members"`) |
| `Kind` | Domain event type for routing (e.g. `"member.added"`, `"document.processed"`) |
| `ActorID` | Who triggered the event (member ID or `"system"`) |
| `FamilyID` | Family scope for multi-tenant isolation |
| `EntityIDs` | Entity IDs involved in the event |
| `Emails` | Email messages to send |

When `Scopes` is set, scope IDs are published directly to NATS. When `Emails` is set, emails are sent via the notification service or direct SMTP. When `Kind` is set and a `Dispatcher` is configured, the event is forwarded for additional processing.

## Notification Service

The notification service is a standalone Connect-RPC server that handles email delivery via SMTP.

### Proto API

```protobuf
service NotificationService {
  rpc SendEmail(SendEmailRequest) returns (SendEmailResponse);
}
```

### Running the service

```bash
# Start dependencies
docker compose up -d

# Run the server
SMTP_HOST=localhost SMTP_PORT=1025 SMTP_FROM=butler@example.com go run ./cmd/notify
```

### Environment Variables

| Variable | Default | Description |
|---|---|---|
| `ADDR` | `:3001` | Server listen address |
| `SMTP_HOST` | `localhost` | SMTP server host |
| `SMTP_PORT` | `1025` | SMTP server port |
| `SMTP_FROM` | `noreply@example.com` | Sender address |
| `SMTP_USERNAME` | | Optional SMTP auth username |
| `SMTP_PASSWORD` | | Optional SMTP auth password |
| `API_KEYS` | | Comma-separated API keys for RPC auth |
| `RATE_LIMIT` | `10` | Requests per second per IP |
| `RATE_BURST` | `20` | Burst allowance per IP |
| `CORS_ORIGINS` | | Comma-separated allowed origins |

### Authentication

When `API_KEYS` is set, all RPC calls require a `Bearer` token in the `Authorization` header:

```go
req := connect.NewRequest(&notifyv1.SendEmailRequest{...})
req.Header().Set("Authorization", "Bearer your-api-key")
client.SendEmail(ctx, req)
```

## Security

Stream invalidations publish **only scope IDs** to NATS with a `nil` payload. No sensitive data flows through the pub/sub layer. Browsers receive a "stale" flag and re-fetch data through their own authenticated requests.

The notification service handles PII (email addresses, message content) internally. Secure it with API key auth and restrict network access.

## Local Development

```bash
# Start NATS and Mailpit
docker compose up -d

# NATS is available at localhost:4222
# Mailpit UI is available at http://localhost:8025
# Mailpit SMTP is available at localhost:1025

# Run tests
task test

# Run the notification server
task run
```

## Project Structure

```
notify.go              Client: NATS direct (scopes) + RPC or SMTP (emails)
email/
  email.go             SMTP sender (Message, Sender interface, SMTPSender)
proto/notify/v1/
  notify.proto         NotificationService { SendEmail }
gen/                   Generated protobuf + Connect-RPC code
server.go              NewServer() — wires handler, auth, middleware
handler.go             Connect-RPC handler (SendEmail -> SMTP)
auth.go                API key auth interceptor (Bearer token)
middleware.go          Logging, security headers, rate limit, CORS
config.go              ServerConfigFromEnv()
cmd/notify/main.go     Server entrypoint with graceful shutdown
docker-compose.yml     NATS + Mailpit
Taskfile.yml           generate, build, test, run, up, down
```

## Commands

```bash
task generate    # Generate protobuf and Connect-RPC code
task build       # Build the notification server binary
task test        # Run all tests
task run         # Run the notification server
task up          # Start docker-compose services
task down        # Stop docker-compose services
task tidy        # Tidy go modules
task lint        # Lint protobuf definitions
```
