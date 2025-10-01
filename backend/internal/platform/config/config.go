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
}

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
		Env:         required("APP_ENV"),
		HTTPAddr:    required("HTTP_ADDR"),
		DatabaseURL: required("DATABASE_URL"),
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

	level := optional("LOG_LEVEL", "info")
	if err := cfg.LogLevel.UnmarshalText([]byte(level)); err != nil {
		errs = append(errs, fmt.Errorf("LOG_LEVEL must be debug, info, warn or error, got %q", level))
	}

	timeout := optional("SHUTDOWN_TIMEOUT", "15s")
	d, err := time.ParseDuration(timeout)
	switch {
	case err != nil:
		errs = append(errs, fmt.Errorf("SHUTDOWN_TIMEOUT must be a duration like 15s, got %q", timeout))
	case d <= 0:
		errs = append(errs, fmt.Errorf("SHUTDOWN_TIMEOUT must be positive, got %q", timeout))
	default:
		cfg.ShutdownTimeout = d
	}

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration:\n%w", errors.Join(errs...))
	}
	return cfg, nil
}
