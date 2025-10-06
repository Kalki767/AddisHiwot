// Command api runs the AddisHiwot HTTP API.
//
// Run with the settings in the environment (see .env.example). With
// -healthcheck it instead asks a running API on HTTP_ADDR whether it is ready
// and exits 0 or 1; the container image has no curl, so Docker uses this.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Kalki767/AddisHiwot/backend/internal/api"
	"github.com/Kalki767/AddisHiwot/backend/internal/auth/argon2id"
	authemail "github.com/Kalki767/AddisHiwot/backend/internal/auth/email"
	authhttp "github.com/Kalki767/AddisHiwot/backend/internal/auth/http"
	"github.com/Kalki767/AddisHiwot/backend/internal/auth/jwttoken"
	authpostgres "github.com/Kalki767/AddisHiwot/backend/internal/auth/postgres"
	authusecase "github.com/Kalki767/AddisHiwot/backend/internal/auth/usecase"
	"github.com/Kalki767/AddisHiwot/backend/internal/platform/config"
	"github.com/Kalki767/AddisHiwot/backend/internal/platform/database"
	"github.com/Kalki767/AddisHiwot/backend/internal/platform/health"
	"github.com/Kalki767/AddisHiwot/backend/internal/platform/httpx"
	"github.com/Kalki767/AddisHiwot/backend/internal/platform/mail"
	"github.com/Kalki767/AddisHiwot/backend/internal/platform/ratelimit"
	usershttp "github.com/Kalki767/AddisHiwot/backend/internal/users/http"
	userspostgres "github.com/Kalki767/AddisHiwot/backend/internal/users/postgres"
	usersusecase "github.com/Kalki767/AddisHiwot/backend/internal/users/usecase"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "check /readyz of a running API and exit")
	flag.Parse()

	if *healthcheck {
		os.Exit(checkReady(os.Getenv("HTTP_ADDR")))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel})).
		With(slog.String("service", "api"), slog.String("env", cfg.Env))

	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	tokens, err := jwttoken.New(cfg.JWTSecret, cfg.AccessTokenTTL)
	if err != nil {
		return err
	}
	sender, err := mail.NewSMTP(cfg.SMTPAddr, cfg.MailFrom)
	if err != nil {
		return err
	}
	usersRepo := userspostgres.NewRepository(pool)
	authService, err := authusecase.NewService(authusecase.Deps{
		Users:      usersRepo,
		Sessions:   authpostgres.NewSessions(pool),
		Codes:      authpostgres.NewCodes(pool),
		Hasher:     argon2id.New(argon2id.OWASPMinimum),
		Tokens:     tokens,
		Notifier:   authemail.New(sender),
		CodeSecret: cfg.AuthCodeSecret,
		Now:        time.Now,
		RefreshTTL: cfg.RefreshTokenTTL,
		Logger:     logger,
	})
	if err != nil {
		return err
	}
	authHandler := authhttp.New(authService, logger, authhttp.Limits{
		// Each IP: bursts of 20, then 20 a minute, across all auth routes.
		PerIP: ratelimit.New(20, 3*time.Second, time.Now),
		// Each email or username: bursts of 5, then 1 a minute.
		PerAccount: ratelimit.New(5, time.Minute, time.Now),
	})

	checker := health.New(pool, 2*time.Second, logger)
	srv := httpx.NewServer(api.NewRouter(api.Deps{
		Logger: logger,
		Health: checker,
		Features: []api.Registrar{
			authHandler,
			usershttp.New(usersusecase.NewService(usersRepo), logger, authHandler.RequireAuth),
		},
	}))
	srv.ErrorLog = slog.NewLogLogger(logger.Handler(), slog.LevelWarn)

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.HTTPAddr, err)
	}
	logger.InfoContext(ctx, "api started", slog.String("addr", ln.Addr().String()))

	err = httpx.Serve(ctx, srv, ln, cfg.ShutdownTimeout, func() {
		logger.InfoContext(ctx, "shutting down: draining requests", slog.String("timeout", cfg.ShutdownTimeout.String()))
		checker.StartDraining()
	})
	if err != nil {
		return err
	}
	logger.InfoContext(ctx, "api stopped")
	return nil
}

// checkReady returns 0 when GET /readyz answers 200, else 1. It always calls
// 127.0.0.1 inside the container and takes only the port from HTTP_ADDR, so the
// API must listen on all interfaces or on loopback (":8080" does both).
func checkReady(addr string) int {
	_, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return 1
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/readyz", port), nil)
	if err != nil {
		return 1
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
