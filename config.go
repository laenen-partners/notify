package notify

import (
	"os"
	"strconv"
	"strings"
)

// ServerConfig holds settings for the notification server.
type ServerConfig struct {
	SMTPHost     string   // SMTP server host (default "localhost")
	SMTPPort     int      // SMTP server port (default 1025)
	SMTPFrom     string   // sender address (default "noreply@example.com")
	SMTPUsername string   // optional SMTP auth username
	SMTPPassword string   // optional SMTP auth password
	APIKeys      []string // API keys for RPC authentication
	RateLimit    float64  // requests per second per IP (0 = disabled)
	RateBurst    int      // burst allowance per IP
	CORSOrigins  []string // allowed CORS origins (empty = no CORS)
}

// ServerConfigFromEnv reads server configuration from environment variables.
func ServerConfigFromEnv() ServerConfig {
	smtpHost := os.Getenv("SMTP_HOST")
	if smtpHost == "" {
		smtpHost = "localhost"
	}

	smtpPort := 1025
	if v := os.Getenv("SMTP_PORT"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			smtpPort = parsed
		}
	}

	smtpFrom := os.Getenv("SMTP_FROM")
	if smtpFrom == "" {
		smtpFrom = "noreply@example.com"
	}

	var apiKeys []string
	if keys := os.Getenv("API_KEYS"); keys != "" {
		for _, k := range strings.Split(keys, ",") {
			if trimmed := strings.TrimSpace(k); trimmed != "" {
				apiKeys = append(apiKeys, trimmed)
			}
		}
	}

	rateLimit := 10.0
	if v := os.Getenv("RATE_LIMIT"); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			rateLimit = parsed
		}
	}

	rateBurst := 20
	if v := os.Getenv("RATE_BURST"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			rateBurst = parsed
		}
	}

	var corsOrigins []string
	if v := os.Getenv("CORS_ORIGINS"); v != "" {
		for _, o := range strings.Split(v, ",") {
			if trimmed := strings.TrimSpace(o); trimmed != "" {
				corsOrigins = append(corsOrigins, trimmed)
			}
		}
	}

	return ServerConfig{
		SMTPHost:     smtpHost,
		SMTPPort:     smtpPort,
		SMTPFrom:     smtpFrom,
		SMTPUsername: os.Getenv("SMTP_USERNAME"),
		SMTPPassword: os.Getenv("SMTP_PASSWORD"),
		APIKeys:      apiKeys,
		RateLimit:    rateLimit,
		RateBurst:    rateBurst,
		CORSOrigins:  corsOrigins,
	}
}
