package session

import (
	"sync"
	"time"
)

// Limiter is a token bucket per key, in memory: perMinute attempts a minute,
// refilled continuously, with a burst of the same size. Logins are limited
// per client address and per username with one each.
type Limiter struct {
	perMinute float64
	maxKeys   int
	now       func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	at     time.Time
}

// NewLimiter makes a limiter. It holds at most maxKeys buckets: beyond that
// it forgets the full ones first — a key that would have every attempt
// anyway — and then the oldest, so that many addresses cannot grow it
// without bound.
func NewLimiter(perMinute, maxKeys int, now func() time.Time) *Limiter {
	if now == nil {
		now = time.Now
	}
	return &Limiter{perMinute: float64(perMinute), maxKeys: maxKeys, now: now, buckets: map[string]*bucket{}}
}

// Allow takes one attempt for key. When none is left it returns false and
// how long until the next one.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= l.maxKeys {
			l.evictLocked(now)
		}
		b = &bucket{tokens: l.perMinute, at: now}
		l.buckets[key] = b
	}
	b.tokens = min(l.perMinute, b.tokens+now.Sub(b.at).Minutes()*l.perMinute)
	b.at = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := time.Duration((1 - b.tokens) / l.perMinute * float64(time.Minute))
	return false, wait
}

func (l *Limiter) evictLocked(now time.Time) {
	for k, b := range l.buckets {
		if b.tokens+now.Sub(b.at).Minutes()*l.perMinute >= l.perMinute {
			delete(l.buckets, k)
		}
	}
	for len(l.buckets) >= l.maxKeys {
		var oldest string
		var at time.Time
		for k, b := range l.buckets {
			if oldest == "" || b.at.Before(at) {
				oldest, at = k, b.at
			}
		}
		delete(l.buckets, oldest)
	}
}
