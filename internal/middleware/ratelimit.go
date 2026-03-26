package middleware

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// bucket tracks token-bucket state for a single client IP.
type bucket struct {
	tokens   float64
	lastSeen time.Time
}

// RateLimiter is an HTTP middleware that enforces per-IP rate limits using
// a token-bucket algorithm. Tokens refill at `rate` per second up to `burst`.
// Stale entries are cleaned up periodically.
type RateLimiter struct {
	rate           float64 // tokens per second
	burst          float64 // max tokens (bucket capacity)
	mu             sync.Mutex
	clients        map[string]*bucket
	maxClients     int // L-10: max entries in the client map
	trustedProxies []*net.IPNet
	log            zerolog.Logger
	done           chan struct{}
	closeOnce      sync.Once // L-11: prevent double-close panic
}

// NewRateLimiter creates a rate limiter. `rps` is the sustained requests/sec
// per IP, `burst` is the maximum burst size. trustedProxies controls which
// peers are allowed to set X-Forwarded-For/X-Real-IP headers for client IP
// extraction (pass nil to always use RemoteAddr). A background goroutine
// prunes stale entries every 5 minutes.
func NewRateLimiter(rps float64, burst int, trustedProxies []*net.IPNet, log zerolog.Logger) *RateLimiter {
	rl := &RateLimiter{
		rate:           rps,
		burst:          float64(burst),
		clients:        make(map[string]*bucket),
		maxClients:     100000, // L-10: cap to prevent unbounded memory growth
		trustedProxies: trustedProxies,
		log:            log,
		done:           make(chan struct{}),
	}
	go rl.cleanup()
	return rl
}

// Wrap returns an http.Handler that enforces the rate limit before delegating
// to the next handler.
func (rl *RateLimiter) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := ClientIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"), r.Header.Get("X-Real-Ip"), rl.trustedProxies)
		if !rl.allow(ip) {
			rl.log.Warn().Str("ip", ip).Msg("rate limited")
			w.Header().Set("Retry-After", "1")
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Close stops the background cleanup goroutine. Safe to call multiple times (L-11).
func (rl *RateLimiter) Close() {
	rl.closeOnce.Do(func() {
		close(rl.done)
	})
}

// allow checks whether the given IP has tokens remaining. It refills tokens
// based on elapsed time since the last request and then consumes one token.
func (rl *RateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, exists := rl.clients[ip]
	if !exists {
		// L-10: Enforce max entries to prevent unbounded memory growth.
		if len(rl.clients) >= rl.maxClients {
			// At capacity — reject new clients until cleanup frees slots.
			return false
		}
		// First request from this IP — start with a full bucket minus 1 token.
		rl.clients[ip] = &bucket{
			tokens:   rl.burst - 1,
			lastSeen: now,
		}
		return true
	}

	// Refill tokens based on elapsed time.
	elapsed := now.Sub(b.lastSeen).Seconds()
	b.tokens += elapsed * rl.rate
	if b.tokens > rl.burst {
		b.tokens = rl.burst
	}
	b.lastSeen = now

	if b.tokens < 1 {
		return false
	}

	b.tokens--
	return true
}

// cleanup periodically removes entries for IPs that haven't been seen in a while.
// This prevents unbounded memory growth.
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-rl.done:
			return
		case <-ticker.C:
			rl.mu.Lock()
			cutoff := time.Now().Add(-10 * time.Minute)
			for ip, b := range rl.clients {
				if b.lastSeen.Before(cutoff) {
					delete(rl.clients, ip)
				}
			}
			rl.mu.Unlock()
		}
	}
}
