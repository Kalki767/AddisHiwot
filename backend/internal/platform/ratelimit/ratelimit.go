// Package ratelimit is a token-bucket rate limiter with one bucket per key
// (an IP address, an email, a username).
//
// A bucket holds up to Burst tokens and refills at Rate tokens per second. Each
// request takes one token; with none left, the request is refused and told how
// long until the next token arrives (the Retry-After). Short bursts are allowed,
// a steady flood is not.
//
// Buckets live in memory, so limits are per API instance and reset on restart;
// a shared store is needed only when several instances run behind a load balancer.
package ratelimit

import (
	"math"
	"sync"
	"time"
)

// Limiter keeps one bucket per key.
type Limiter struct {
	rate  float64 // tokens per second
	burst float64
	now   func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
	calls   int
}

type bucket struct {
	tokens float64
	last   time.Time
}

// sweepEvery is how many calls pass between removals of idle buckets.
const sweepEvery = 1024

// New returns a limiter that allows burst requests at once and then one every
// per (for example New(10, time.Minute/30): bursts of 10, then 30 a minute).
func New(burst int, per time.Duration, now func() time.Time) *Limiter {
	return &Limiter{rate: 1 / per.Seconds(), burst: float64(burst), now: now, buckets: map[string]*bucket{}}
}

// Allow takes a token from key's bucket. When none is left it returns false
// and how long until one is.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()

	l.calls++
	if l.calls%sweepEvery == 0 {
		l.sweep(now)
	}

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens = math.Min(l.burst, b.tokens+elapsed*l.rate)
	}
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	// Round up: a wait that is a hair too short sends the client back too early.
	wait := time.Duration(math.Ceil((1 - b.tokens) / l.rate * float64(time.Second)))
	return false, wait
}

// sweep forgets buckets that have refilled completely: forgetting them changes
// nothing, and it keeps memory bounded however many keys appear.
func (l *Limiter) sweep(now time.Time) {
	for key, b := range l.buckets {
		if b.tokens+now.Sub(b.last).Seconds()*l.rate >= l.burst {
			delete(l.buckets, key)
		}
	}
}

// Len reports how many buckets are held (for tests and metrics).
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}
