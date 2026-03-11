package notify

import (
	"fmt"
	"net/http"

	"connectrpc.com/connect"

	"github.com/laenen-partners/notify/email"
	"github.com/laenen-partners/notify/gen/notify/v1/notifyv1connect"
)

// NewServer creates an http.Handler that serves the NotificationService RPC,
// health endpoints, and applies the middleware stack.
func NewServer(cfg ServerConfig) (http.Handler, error) {
	sender := email.NewSMTPSender(email.SMTPConfig{
		Host:     cfg.SMTPHost,
		Port:     cfg.SMTPPort,
		From:     cfg.SMTPFrom,
		Username: cfg.SMTPUsername,
		Password: cfg.SMTPPassword,
	})

	mux := http.NewServeMux()

	// Health check endpoints (unauthenticated).
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	// Mount Connect-RPC handler with optional auth interceptor.
	var opts []connect.HandlerOption
	if len(cfg.APIKeys) > 0 {
		opts = append(opts, connect.WithInterceptors(NewAuthInterceptor(cfg.APIKeys)))
	}
	path, rpcHandler := notifyv1connect.NewNotificationServiceHandler(
		NewHandler(sender), opts...,
	)
	mux.Handle(path, rpcHandler)

	// Apply middleware stack.
	var handler http.Handler = mux
	handler = SecurityHeaders(handler)
	handler = RequestLogging(handler)
	if len(cfg.CORSOrigins) > 0 {
		handler = CORS(cfg.CORSOrigins)(handler)
	}
	if cfg.RateLimit > 0 {
		handler = RateLimit(cfg.RateLimit, cfg.RateBurst)(handler)
	}

	return handler, nil
}
