package server

import (
	"sync"
	"time"
)

type rateBucket struct {
	tokens float64
	last   time.Time
}

type clientRateLimiter struct {
	mu        sync.Mutex
	perSecond float64
	burst     float64
	buckets   map[string]rateBucket
}

func newClientRateLimiter(perMinute, burst int) *clientRateLimiter {
	return &clientRateLimiter{perSecond: float64(perMinute) / 60, burst: float64(burst), buckets: make(map[string]rateBucket)}
}

func (l *clientRateLimiter) Allow(clientID string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	bucket, ok := l.buckets[clientID]
	if !ok {
		bucket = rateBucket{tokens: l.burst, last: now}
	}
	bucket.tokens += now.Sub(bucket.last).Seconds() * l.perSecond
	if bucket.tokens > l.burst {
		bucket.tokens = l.burst
	}
	bucket.last = now
	allowed := bucket.tokens >= 1
	if allowed {
		bucket.tokens--
	}
	l.buckets[clientID] = bucket
	return allowed
}
