package notify

import (
	"context"

	"connectrpc.com/connect"

	"github.com/laenen-partners/notify/email"
	notifyv1 "github.com/laenen-partners/notify/gen/notify/v1"
	"github.com/laenen-partners/notify/gen/notify/v1/notifyv1connect"
)

// Handler implements the Connect-RPC NotificationServiceHandler.
type Handler struct {
	notifyv1connect.UnimplementedNotificationServiceHandler
	email email.Sender
}

// NewHandler creates a Connect-RPC handler backed by the given email sender.
func NewHandler(emailSender email.Sender) *Handler {
	return &Handler{email: emailSender}
}

// SendEmail sends an email via the configured SMTP backend.
func (h *Handler) SendEmail(_ context.Context, req *connect.Request[notifyv1.SendEmailRequest]) (*connect.Response[notifyv1.SendEmailResponse], error) {
	msg := email.Message{
		To:      req.Msg.To,
		Subject: req.Msg.Subject,
		HTML:    req.Msg.Html,
		Text:    req.Msg.Text,
	}

	if err := h.email.Send(msg); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&notifyv1.SendEmailResponse{}), nil
}
