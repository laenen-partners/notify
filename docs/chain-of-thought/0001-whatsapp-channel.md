# 0001 — WhatsApp Channel (Two-Way Conversational)

## Status

Draft

## Context

The notify library currently supports three outbound channels:

1. **NATS scope invalidations** — direct publish, nil payload (fast path).
2. **Email** — via notification service Connect-RPC or direct SMTP.
3. **Dispatcher interface** — pluggable forwarding for other channels.

We need to add WhatsApp as a first-class channel. Unlike email (fire-and-forget),
WhatsApp is a **two-way conversational interface**:

- Users initiate conversations by sending messages and attachments.
- A conversational AI (backed by threads) processes inbound messages and replies.
- The system sends **inbox items** (action-required templates) and
  **notifications** (informational templates) to users proactively.
- User replies to inbox items feed back into the conversational flow.

## Two Distinct Flows

### Conversational (user-initiated)

```
User sends WhatsApp message / attachment
  → Meta webhook POST
  → resolve phone → member
  → find or create thread
  → conversational AI processes (with thread context)
  → AI reply sent back via WhatsApp (free-form, within 24h window)
```

### System-initiated

```
Inbox item created (e.g. "sign this document")
  → notify.Event with Kind + WhatsApp template message
  → user taps / replies
  → routes into conversational flow above

Notification (e.g. "Pascal commented on …")
  → notify.Event with WhatsApp template message
  → informational, may or may not trigger a reply
```

## Identity Resolution

Users register to use the service via WhatsApp, so the phone → member mapping
is established at onboarding. One phone number maps to one member and one family
in the common case.

### `member_phones` table

| Column      | Type      | Notes                       |
|-------------|-----------|-----------------------------|
| phone       | text (PK) | E.164 format, unique        |
| member_id   | uuid      |                             |
| family_id   | uuid      |                             |
| verified_at | timestamp |                             |

If a member belongs to multiple families, this is handled at onboarding
("which family are you messaging about?") and the active context is stored.

### Inbound resolution flow

```
phone_number
  → member_phones → member_id, family_id
  → find active whatsapp_thread for member_id
     → exists? → resume thread
     → doesn't exist? → create new thread + new AI thread
```

## Thread Management

Every WhatsApp conversation maps to a thread where the AI context lives.

### `whatsapp_threads` table

| Column          | Type      | Notes                                   |
|-----------------|-----------|-----------------------------------------|
| id              | uuid (PK) |                                         |
| member_id       | uuid      | FK → members                            |
| family_id       | uuid      | FK → families                           |
| ai_thread_id    | text      | AI backend's thread/conversation ID     |
| status          | enum      | active, archived                        |
| last_message_at | timestamp |                                         |
| created_at      | timestamp |                                         |

## Package Layout

### `notify/whatsapp/` — delivery library (lives in this repo)

```
whatsapp/
├── whatsapp.go    — Message type, Sender interface, CloudAPISender implementation
├── webhook.go     — HTTP handler: parses Meta webhook payloads (GET verify + POST deliver)
├── media.go       — download attachments from Meta's media API
└── types.go       — InboundMessage, InboundMedia, delivery/read status types
```

### Application code — conversational logic (lives in the app repo)

```
conversation/
├── router.go      — phone → member → thread resolution
├── handler.go     — implements whatsapp.InboundHandler; feeds messages to AI, sends replies
└── thread.go      — thread lifecycle management

inbox/
└── inbox.go       — creates inbox items, uses notify.Client to send WhatsApp templates
```

## API Surface

### Outbound types (`whatsapp/whatsapp.go`)

```go
// Message represents an outbound WhatsApp message.
type Message struct {
    To           string            // recipient phone in E.164 format
    TemplateID   string            // for business-initiated messages (outside 24h window)
    Parameters   map[string]string // template variable substitutions
    Text         string            // for free-form replies (within 24h window)
    CallbackData string            // echoed back on reply (max 256 chars)
}

// Sender sends outbound WhatsApp messages via the Meta Cloud API.
type Sender interface {
    Send(ctx context.Context, msg Message) error
}

// CloudAPISender implements Sender using the Meta Cloud API.
// Calls POST https://graph.facebook.com/v21.0/{phone-number-id}/messages
type CloudAPISender struct {
    phoneNumberID string
    accessToken   string
    httpClient    *http.Client
}
```

### Inbound types (`whatsapp/types.go`)

```go
// InboundMessage represents a message received from a WhatsApp user.
type InboundMessage struct {
    From         string        // sender phone number (E.164)
    Body         string        // text content
    MessageID    string        // WhatsApp message ID (for read receipts, replies)
    Timestamp    time.Time
    Media        []InboundMedia
    CallbackData string        // from biz_opaque_callback_data, if present
}

// InboundMedia represents an attachment on an inbound message.
type InboundMedia struct {
    ID       string // Meta media ID (use MediaDownloader to fetch bytes)
    MimeType string
    Caption  string
}
```

### Webhook handler (`whatsapp/webhook.go`)

```go
// InboundHandler processes incoming WhatsApp messages.
type InboundHandler interface {
    HandleWhatsApp(ctx context.Context, msg InboundMessage) error
}

// WebhookHandler returns an http.Handler for Meta's webhook.
// GET  → hub.verify_token challenge (webhook registration).
// POST → message delivery, status updates.
func WebhookHandler(verifyToken string, handler InboundHandler) http.Handler
```

### Media downloader (`whatsapp/media.go`)

```go
// MediaDownloader fetches media bytes from Meta's media API.
type MediaDownloader struct {
    accessToken string
    httpClient  *http.Client
}

// Download retrieves the media content for the given Meta media ID.
func (d *MediaDownloader) Download(ctx context.Context, mediaID string) (io.ReadCloser, string, error)
```

## Changes to `notify.go`

### Event struct

```go
type Event struct {
    Scopes    []string
    Kind      string
    ActorID   string
    FamilyID  string
    EntityIDs []string
    Emails    []email.Message
    WhatsApp  []whatsapp.Message  // NEW
}
```

### New option

```go
func WithWhatsApp(s whatsapp.Sender) Option {
    return func(c *Client) { c.whatsapp = s }
}
```

### Notify method

Adds a loop after the email loop:

```go
for _, msg := range event.WhatsApp {
    if err := c.sendWhatsApp(ctx, msg); err != nil {
        return fmt.Errorf("notify: send whatsapp to %q: %w", msg.To, err)
    }
}
```

## Changes to `server.go`

Mount the webhook handler:

```go
if cfg.WhatsAppVerifyToken != "" {
    mux.Handle("/webhooks/whatsapp", whatsapp.WebhookHandler(
        cfg.WhatsAppVerifyToken,
        cfg.WhatsAppInboundHandler,
    ))
}
```

## Application-Level Inbound Handler

This lives in the application, not in the notify library.

```go
type Handler struct {
    members  MemberPhoneStore
    threads  ThreadStore
    ai       AIClient
    whatsapp whatsapp.Sender
    media    whatsapp.MediaDownloader
}

func (h *Handler) HandleWhatsApp(ctx context.Context, msg whatsapp.InboundMessage) error {
    // 1. Resolve identity
    member, err := h.members.ByPhone(ctx, msg.From)
    if err != nil {
        return h.whatsapp.Send(ctx, whatsapp.Message{
            To:   msg.From,
            Text: "We don't recognise this number. Register at ...",
        })
    }

    // 2. Find or create thread
    thread, err := h.threads.ActiveForMember(ctx, member.ID)
    if err != nil {
        thread, err = h.threads.Create(ctx, member.ID, member.FamilyID)
    }

    // 3. Handle attachments
    var attachments []Attachment
    for _, m := range msg.Media {
        data, _ := h.media.Download(ctx, m.ID)
        // store in object store, get URL
        attachments = append(attachments, ...)
    }

    // 4. Send to conversational AI with thread context
    aiReply, err := h.ai.Message(ctx, AIRequest{
        ThreadID:    thread.AIThreadID,
        Text:        msg.Body,
        Attachments: attachments,
        MemberID:    member.ID,
        FamilyID:    member.FamilyID,
    })

    // 5. Send AI reply back via WhatsApp
    return h.whatsapp.Send(ctx, whatsapp.Message{
        To:   msg.From,
        Text: aiReply.Text,
    })
}
```

## Inbox Items → WhatsApp Templates

When the system creates an inbox item, it sends a pre-approved Meta template:

```go
err := notifyClient.Notify(ctx, notify.Event{
    Scopes:   []string{fmt.Sprintf("family:%s:inbox", item.FamilyID)},
    Kind:     "inbox.created",
    FamilyID: item.FamilyID,
    WhatsApp: []whatsapp.Message{{
        To:         recipientPhone,
        TemplateID: "inbox_action_required",
        Parameters: map[string]string{
            "1": item.Title,
            "2": item.ActionLabel,
        },
        CallbackData: fmt.Sprintf("inbox:%s", item.ID),
    }},
})
```

When the user replies, `CallbackData` is echoed back by Meta, so the inbound
handler can link the reply to the specific inbox item:

```go
if strings.HasPrefix(msg.CallbackData, "inbox:") {
    itemID := strings.TrimPrefix(msg.CallbackData, "inbox:")
    // load inbox item, inject into AI thread context
}
```

## Meta Cloud API Constraints

- **Template messages** are required for business-initiated conversations
  (outside the 24h window). Templates must be pre-approved by Meta.
- **Free-form messages** (text, media) are allowed within 24h of the user's
  last message.
- **`biz_opaque_callback_data`** (max 256 chars) is attached to outbound
  messages and echoed back on replies to that message.
- **Webhook verification** requires a GET endpoint that echoes back
  `hub.challenge` when `hub.verify_token` matches.
- **Media downloads** require a two-step process: get the media URL from the
  media ID, then download the content with the access token.

## Open Questions

1. **Proto definition** — should `SendWhatsApp` be added to the protobuf service
   definition, or keep WhatsApp as a Go-only package for now?
2. **Rate limiting** — Meta enforces per-phone-number rate limits. Do we need
   client-side throttling?
3. **Delivery receipts** — Meta sends status webhooks (sent, delivered, read).
   Do we surface these to the application?
4. **Multi-family disambiguation** — if we ever need to support one phone across
   multiple families, what's the UX? Interactive list message?
