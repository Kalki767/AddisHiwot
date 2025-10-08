// Package config reads the service settings from environment variables.
//
// Every problem is collected and reported at once, so a misconfigured service
// stops at start-up with a full list instead of failing later, one setting at a time.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Environment names where the service runs.
const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

// Config holds the settings of the API process.
type Config struct {
	Env             string        // APP_ENV: development or production
	HTTPAddr        string        // HTTP_ADDR: address to listen on, e.g. ":8080"
	DatabaseURL     string        // DATABASE_URL: postgres://user:pass@host:port/db
	LogLevel        slog.Level    // LOG_LEVEL: debug, info, warn or error (default info)
	ShutdownTimeout time.Duration // SHUTDOWN_TIMEOUT: time allowed to finish requests on stop (default 15s)
	JWTSecret       []byte        // JWT_SECRET: signs access tokens; at least 32 bytes
	AccessTokenTTL  time.Duration // ACCESS_TOKEN_TTL: access token lifetime (default 15m)
	RefreshTokenTTL time.Duration // REFRESH_TOKEN_TTL: refresh token lifetime (default 720h = 30 days)
	AuthCodeSecret  []byte        // AUTH_CODE_SECRET: keys the HMAC of email codes; at least 32 bytes
	SMTPAddr        string        // SMTP_ADDR: host:port of the mail server (Mailpit in development)
	MailFrom        string        // MAIL_FROM: sender, e.g. "Addis Hiwot <no-reply@addishiwot.test>"
	// GOOGLE_CLIENT_IDS: comma-separated OAuth client IDs whose Google ID tokens
	// are accepted (the app's Android, iOS and web IDs). Empty turns Google sign-in off.
	GoogleClientIDs []string
}

var googleClientID = regexp.MustCompile(`^[0-9A-Za-z-]+\.apps\.googleusercontent\.com$`)

// MinJWTSecretBytes is the shortest accepted JWT_SECRET or AUTH_CODE_SECRET (256 bits).
const MinJWTSecretBytes = 32

// Load reads the settings through getenv (os.Getenv in production, a map in tests).
func Load(getenv func(string) string) (Config, error) {
	var errs []error
	required := func(key string) string {
		v := strings.TrimSpace(getenv(key))
		if v == "" {
			errs = append(errs, fmt.Errorf("%s is required", key))
		}
		return v
	}
	optional := func(key, def string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return def
	}

	cfg := Config{
		Env:            required("APP_ENV"),
		HTTPAddr:       required("HTTP_ADDR"),
		DatabaseURL:    required("DATABASE_URL"),
		JWTSecret:      []byte(required("JWT_SECRET")),
		AuthCodeSecret: []byte(required("AUTH_CODE_SECRET")),
		SMTPAddr:       required("SMTP_ADDR"),
		MailFrom:       required("MAIL_FROM"),
	}
	// Secrets are never echoed back, only their length.
	for key, secret := range map[string][]byte{"JWT_SECRET": cfg.JWTSecret, "AUTH_CODE_SECRET": cfg.AuthCodeSecret} {
		if n := len(secret); n > 0 && n < MinJWTSecretBytes {
			errs = append(errs, fmt.Errorf("%s must have at least %d bytes, got %d (generate one with: openssl rand -base64 48)", key, MinJWTSecretBytes, n))
		}
	}
	if cfg.SMTPAddr != "" {
		if _, _, err := net.SplitHostPort(cfg.SMTPAddr); err != nil {
			errs = append(errs, fmt.Errorf("SMTP_ADDR must look like host:port, got %q", cfg.SMTPAddr))
		}
	}

	if cfg.Env != "" && cfg.Env != EnvDevelopment && cfg.Env != EnvProduction {
		errs = append(errs, fmt.Errorf("APP_ENV must be %q or %q, got %q", EnvDevelopment, EnvProduction, cfg.Env))
	}
	if cfg.HTTPAddr != "" {
		// The port must be a number: the container health check reads it the same way.
		_, port, err := net.SplitHostPort(cfg.HTTPAddr)
		if n, convErr := strconv.Atoi(port); err != nil || convErr != nil || n < 1 || n > 65535 {
			errs = append(errs, fmt.Errorf("HTTP_ADDR must look like host:port or :port with a port from 1 to 65535, got %q", cfg.HTTPAddr))
		}
	}
	if cfg.DatabaseURL != "" {
		// The URL holds a password, so it is never echoed back in an error.
		if u, err := url.Parse(cfg.DatabaseURL); err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
			errs = append(errs, errors.New("DATABASE_URL must be a postgres:// URL"))
		}
	}

	for id := range strings.SplitSeq(getenv("GOOGLE_CLIENT_IDS"), ",") {
		if id = strings.TrimSpace(id); id == "" {
			continue
		}
		if !googleClientID.MatchString(id) {
			errs = append(errs, fmt.Errorf("GOOGLE_CLIENT_IDS must list IDs ending in .apps.googleusercontent.com, got %q", id))
		}
		cfg.GoogleClientIDs = append(cfg.GoogleClientIDs, id)
	}

	level := optional("LOG_LEVEL", "info")
	if err := cfg.LogLevel.UnmarshalText([]byte(level)); err != nil {
		errs = append(errs, fmt.Errorf("LOG_LEVEL must be debug, info, warn or error, got %q", level))
	}

	duration := func(key, def string) time.Duration {
		text := optional(key, def)
		d, err := time.ParseDuration(text)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("%s must be a duration like %s, got %q", key, def, text))
		case d <= 0:
			errs = append(errs, fmt.Errorf("%s must be positive, got %q", key, text))
		}
		return d
	}
	cfg.ShutdownTimeout = duration("SHUTDOWN_TIMEOUT", "15s")
	cfg.AccessTokenTTL = duration("ACCESS_TOKEN_TTL", "15m")
	cfg.RefreshTokenTTL = duration("REFRESH_TOKEN_TTL", "720h")
	if cfg.AccessTokenTTL > 0 && cfg.RefreshTokenTTL > 0 && cfg.RefreshTokenTTL <= cfg.AccessTokenTTL {
		errs = append(errs, errors.New("REFRESH_TOKEN_TTL must be longer than ACCESS_TOKEN_TTL"))
	}

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration:\n%w", errors.Join(errs...))
	}
	return cfg, nil
}
