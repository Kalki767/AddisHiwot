// Package api builds the HTTP handler of the API process: routes plus the
// middleware every request goes through.
package api

import (
	"log/slog"
	"net/http"

	"github.com/Kalki767/AddisHiwot/backend/internal/platform/health"
	"github.com/Kalki767/AddisHiwot/backend/internal/platform/httpx"
)

// Registrar adds a feature's routes, such as (*authhttp.Handler).Register.
type Registrar interface {
	Register(mux *http.ServeMux)
}

// Deps are the parts the routes need.
type Deps struct {
	Logger   *slog.Logger
	Health   *health.Checker
	Features []Registrar // each feature's HTTP handlers
}

// NewRouter returns the API handler. Every request gets a request ID, every
// error uses the JSON envelope, and a panic becomes a logged 500. The
// health endpoints skip the access log: Docker calls /readyz every few seconds,
// and logging each call would bury the real traffic (a failing check still
// logs its own warning). Everything else is access-logged.
func NewRouter(d Deps) http.Handler {
	app := http.NewServeMux()
	for _, f := range d.Features {
		f.Register(app)
	}

	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", d.Health.Live)
	root.HandleFunc("GET /readyz", d.Health.Ready)
	// Without these, other methods would fall through to "/" and get 404 instead of 405.
	root.HandleFunc("/healthz", onlyGet)
	root.HandleFunc("/readyz", onlyGet)
	root.Handle("/", httpx.AccessLog(d.Logger, httpx.Recover(d.Logger, app)))
	// MuxErrors turns the mux's plain-text 404 and 405 into the JSON error envelope.
	return httpx.RequestID(httpx.MuxErrors(root))
}

func onlyGet(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Allow", "GET, HEAD")
	http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
}
