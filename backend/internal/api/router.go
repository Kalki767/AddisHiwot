// Package api builds the HTTP handler of the API process: routes plus the
// middleware every request goes through.
package api

import (
	"log/slog"
	"net/http"

	"github.com/Kalki767/AddisHiwot/backend/internal/platform/health"
	"github.com/Kalki767/AddisHiwot/backend/internal/platform/httpx"
)

// Deps are the parts the routes need.
type Deps struct {
	Logger *slog.Logger
	Health *health.Checker
}

// NewRouter returns the API handler. Every request gets a request ID. The
// health endpoints skip the access log: Docker calls /readyz every few seconds,
// and logging each call would bury the real traffic (a failing check still
// logs its own warning). Everything else is access-logged.
func NewRouter(d Deps) http.Handler {
	app := http.NewServeMux() // feature routes are added here from Day 8 on

	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", d.Health.Live)
	root.HandleFunc("GET /readyz", d.Health.Ready)
	// Without these, other methods would fall through to "/" and get 404 instead of 405.
	root.HandleFunc("/healthz", onlyGet)
	root.HandleFunc("/readyz", onlyGet)
	root.Handle("/", httpx.AccessLog(d.Logger, app))
	return httpx.RequestID(root)
}

func onlyGet(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Allow", "GET, HEAD")
	http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
}
