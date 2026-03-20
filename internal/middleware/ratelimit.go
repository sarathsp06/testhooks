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
	rate    float64 // tokens per second
	burst   float64 // max tokens (bucket capacity)
	mu      sync.Mutex
	clients map[string]*bucket
	log     zerolog.Logger
	done    chan struct{}
}

// NewRateLimiter creates a rate limiter. `rps` is the sustained requests/sec
// per IP, `burst` is the maximum burst size. A background goroutine prunes
// stale entries every 5 minutes.
func NewRateLimiter(rps float64, burst int, log zerolog.Logger) *RateLimiter {
	rl := &RateLimiter{
		rate:    rps,
		burst:   float64(burst),
		clients: make(map[string]*bucket),
		log:     log,
		done:    make(chan struct{}),
	}
	go rl.cleanup()
	return rl
}

// Wrap returns an http.Handler that enforces the rate limit before delegating
// to the next handler.
func (rl *RateLimiter) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !rl.allow(ip) {
			rl.log.Warn().Str("ip", ip).Msg("rate limited")
			w.Header().Set("Retry-After", "1")
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Close stops the background cleanup goroutine.
func (rl *RateLimiter) Close() {
	close(rl.done)
}

// allow checks whether the given IP has tokens remaining. It refills tokens
// based on elapsed time since the last request and then consumes one token.
func (rl *RateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, exists := rl.clients[ip]
	if !exists {
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

// clientIP extracts the client IP from the request. It checks X-Forwarded-For
// and X-Real-IP headers first (for reverse proxies), then falls back to
// RemoteAddr.
func clientIP(r *http.Request) string {
	// Check X-Forwarded-For (first IP in the chain).
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For can be "client, proxy1, proxy2"
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' {
				return xff[:i]
			}
		}
		return xff
	}

	// Check X-Real-IP.
	if xri := r.Header.Get("X-Real-Ip"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr (strip port).
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
