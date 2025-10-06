package httpx

import (
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"time"
)

// CodeRateLimited is the error code for 429 Too Many Requests.
const CodeRateLimited = "RATE_LIMITED"

// Allower is a rate limiter (see internal/platform/ratelimit).
type Allower interface {
	Allow(key string) (bool, time.Duration)
}

// WriteTooManyRequests answers 429 with Retry-After in whole seconds, rounded up.
func WriteTooManyRequests(w http.ResponseWriter, r *http.Request, logger *slog.Logger, wait time.Duration) {
	seconds := max(1, int(math.Ceil(wait.Seconds())))
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	WriteError(w, r, logger, NewError(http.StatusTooManyRequests, CodeRateLimited,
		"Too many attempts. Try again in "+strconv.Itoa(seconds)+" seconds."))
}

// LimitByIP lets each client IP through at the limiter's rate.
func LimitByIP(l Allower, logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok, wait := l.Allow("ip:" + ClientIP(r)); !ok {
			WriteTooManyRequests(w, r, logger, wait)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ClientIP is the address the request came from. It reads the TCP peer, not
// X-Forwarded-For, which any client can forge; trusting a proxy's header is
// added together with the proxy, before deployment.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
