package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func valid() map[string]string {
	return map[string]string{
		"APP_ENV":          "development",
		"HTTP_ADDR":        ":8080",
		"DATABASE_URL":     "postgres://u:secret-pw@localhost:5433/db?sslmode=disable",
		"JWT_SECRET":       "a-dev-secret-that-is-at-least-32-bytes",
		"AUTH_CODE_SECRET": "another-dev-secret-at-least-32-bytes",
		"SMTP_ADDR":        "mailpit:1025",
		"MAIL_FROM":        "Addis Hiwot <no-reply@addishiwot.test>",
	}
}

func TestLoadValidWithDefaults(t *testing.T) {
	cfg, err := Load(env(valid()))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Env != EnvDevelopment || cfg.HTTPAddr != ":8080" {
		t.Errorf("got Env=%q HTTPAddr=%q", cfg.Env, cfg.HTTPAddr)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel default = %v, want info", cfg.LogLevel)
	}
	if cfg.ShutdownTimeout != 15*time.Second {
		t.Errorf("ShutdownTimeout default = %v, want 15s", cfg.ShutdownTimeout)
	}
	if cfg.AccessTokenTTL != 15*time.Minute || cfg.RefreshTokenTTL != 30*24*time.Hour {
		t.Errorf("token TTL defaults = %v, %v", cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	}
	if string(cfg.JWTSecret) != "a-dev-secret-that-is-at-least-32-bytes" {
		t.Error("JWT_SECRET not read")
	}
}

func TestLoadOptionalValues(t *testing.T) {
	m := valid()
	m["LOG_LEVEL"] = "debug"
	m["SHUTDOWN_TIMEOUT"] = "3s"
	cfg, err := Load(env(m))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LogLevel != slog.LevelDebug || cfg.ShutdownTimeout != 3*time.Second {
		t.Errorf("got LogLevel=%v ShutdownTimeout=%v", cfg.LogLevel, cfg.ShutdownTimeout)
	}
}

func TestLoadAcceptsHostForms(t *testing.T) {
	for _, addr := range []string{":8080", "127.0.0.1:8080", "[::1]:8080", "0.0.0.0:1"} {
		m := valid()
		m["HTTP_ADDR"] = addr
		if _, err := Load(env(m)); err != nil {
			t.Errorf("HTTP_ADDR=%q rejected: %v", addr, err)
		}
	}
}

func TestLoadReportsEveryMissingValueAtOnce(t *testing.T) {
	_, err := Load(env(map[string]string{}))
	if err == nil {
		t.Fatal("Load with an empty environment succeeded")
	}
	for _, key := range []string{"APP_ENV", "HTTP_ADDR", "DATABASE_URL", "JWT_SECRET", "AUTH_CODE_SECRET", "SMTP_ADDR", "MAIL_FROM"} {
		if !strings.Contains(err.Error(), key+" is required") {
			t.Errorf("error does not mention %s:\n%v", key, err)
		}
	}
}

func TestLoadRejectsBadValues(t *testing.T) {
	tests := []struct {
		key, value, want string
	}{
		{"APP_ENV", "staging", "APP_ENV must be"},
		{"HTTP_ADDR", "8080", "HTTP_ADDR must look like"},
		{"HTTP_ADDR", ":http", "HTTP_ADDR must look like"},
		{"HTTP_ADDR", ":0", "HTTP_ADDR must look like"},
		{"HTTP_ADDR", ":65536", "HTTP_ADDR must look like"},
		{"DATABASE_URL", "mysql://u:p@h/db", "DATABASE_URL must be a postgres:// URL"},
		{"LOG_LEVEL", "loud", "LOG_LEVEL must be"},
		{"SHUTDOWN_TIMEOUT", "soon", "SHUTDOWN_TIMEOUT must be a duration"},
		{"SHUTDOWN_TIMEOUT", "-1s", "SHUTDOWN_TIMEOUT must be positive"},
		{"JWT_SECRET", "too-short", "JWT_SECRET must have at least 32 bytes, got 9"},
		{"AUTH_CODE_SECRET", "too-short", "AUTH_CODE_SECRET must have at least 32 bytes, got 9"},
		{"SMTP_ADDR", "mailpit", "SMTP_ADDR must look like host:port"},
		{"ACCESS_TOKEN_TTL", "soon", "ACCESS_TOKEN_TTL must be a duration"},
		{"REFRESH_TOKEN_TTL", "0s", "REFRESH_TOKEN_TTL must be positive"},
		{"REFRESH_TOKEN_TTL", "10m", "REFRESH_TOKEN_TTL must be longer than ACCESS_TOKEN_TTL"},
		{"APP_ENV", "   ", "APP_ENV is required"},
	}
	for _, tt := range tests {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			m := valid()
			m[tt.key] = tt.value
			_, err := Load(env(m))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestLoadNeverPrintsTheJWTSecret(t *testing.T) {
	m := valid()
	m["JWT_SECRET"] = "short-secret"
	_, err := Load(env(m))
	if err == nil || strings.Contains(err.Error(), "short-secret") {
		t.Errorf("err = %v, want an error without the secret", err)
	}
}

func TestLoadNeverPrintsTheDatabasePassword(t *testing.T) {
	m := valid()
	m["DATABASE_URL"] = "mysql://u:secret-pw@h/db"
	m["APP_ENV"] = ""
	_, err := Load(env(m))
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "secret-pw") {
		t.Errorf("error leaks the password: %v", err)
	}
}
