package ratelimit

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newLimiter(burst int, per time.Duration) (*Limiter, *clock) {
	c := &clock{t: time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)}
	return New(burst, per, c.now), c
}

func TestBurstThenRefuse(t *testing.T) {
	l, _ := newLimiter(3, 10*time.Second)
	for i := range 3 {
		if ok, _ := l.Allow("ip"); !ok {
			t.Fatalf("request %d of the burst refused", i+1)
		}
	}
	ok, wait := l.Allow("ip")
	if ok || wait != 10*time.Second {
		t.Errorf("4th request: ok=%t wait=%v, want refused with 10s", ok, wait)
	}
}

func TestRefillsAtTheRate(t *testing.T) {
	l, c := newLimiter(2, 10*time.Second)
	l.Allow("ip")
	l.Allow("ip")
	c.t = c.t.Add(4 * time.Second)
	if ok, wait := l.Allow("ip"); ok || wait != 6*time.Second {
		t.Errorf("after 4s: ok=%t wait=%v, want refused with 6s left", ok, wait)
	}
	c.t = c.t.Add(6 * time.Second)
	if ok, _ := l.Allow("ip"); !ok {
		t.Error("after 10s a token must be back")
	}
	c.t = c.t.Add(time.Hour)
	for i := range 2 {
		if ok, _ := l.Allow("ip"); !ok {
			t.Fatalf("after an hour, request %d refused", i+1)
		}
	}
	if ok, _ := l.Allow("ip"); ok {
		t.Error("the bucket must not fill beyond its burst")
	}
}

func TestKeysAreIndependent(t *testing.T) {
	l, _ := newLimiter(1, time.Minute)
	l.Allow("a@example.com")
	if ok, _ := l.Allow("b@example.com"); !ok {
		t.Error("one key's limit blocked another key")
	}
}

func TestIdleBucketsAreForgotten(t *testing.T) {
	l, c := newLimiter(2, time.Second)
	for i := range sweepEvery - 1 {
		l.Allow(fmt.Sprintf("ip-%d", i))
	}
	c.t = c.t.Add(time.Minute) // every bucket has refilled
	l.Allow("trigger")         // the sweepEvery-th call sweeps
	if n := l.Len(); n > 1 {
		t.Errorf("%d buckets left, want only the newest", n)
	}
}

func TestSafeForConcurrentUse(t *testing.T) {
	l, _ := newLimiter(100, time.Hour)
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for range 500 {
		wg.Go(func() {
			if ok, _ := l.Allow("ip"); ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if allowed != 100 {
		t.Errorf("allowed %d of 500 concurrent requests, want exactly the burst of 100", allowed)
	}
}
