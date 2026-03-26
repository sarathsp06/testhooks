package middleware

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strings"

	"github.com/rs/zerolog"
)

// TokenAuth returns middleware that validates a Bearer token on all requests
// except the webhook capture path (/h/) which must remain publicly accessible.
// If token is empty, auth is disabled and all requests pass through.
// Auth failures are logged at Warn level with the client IP for auditing.
func TokenAuth(token string, log zerolog.Logger, trustedProxies []*net.IPNet) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Auth disabled if no token configured.
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Webhook capture endpoints are always public — external systems
			// sending webhooks cannot be expected to authenticate.
			if strings.HasPrefix(r.URL.Path, "/h/") {
				next.ServeHTTP(w, r)
				return
			}

			// Health check endpoint is always public — container orchestrators
			// need to probe without credentials.
			if r.URL.Path == "/healthz" {
				next.ServeHTTP(w, r)
				return
			}

			// Static SPA assets don't require auth.
			if r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/assets/") || r.URL.Path == "/favicon.ico" {
				next.ServeHTTP(w, r)
				return
			}

			clientIP := ClientIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"), r.Header.Get("X-Real-Ip"), trustedProxies)

			// WebSocket upgrade requests pass token via Sec-WebSocket-Protocol header
			// (browsers can't set Authorization on WS). The subprotocol format is
			// "auth.<token>" — the server selects it to echo back.
			if strings.HasPrefix(r.URL.Path, "/ws/") {
				protocols := r.Header.Get("Sec-WebSocket-Protocol")
				if protocols != "" {
					for _, proto := range strings.Split(protocols, ",") {
						proto = strings.TrimSpace(proto)
						if strings.HasPrefix(proto, "auth.") {
							candidate := strings.TrimPrefix(proto, "auth.")
							if subtle.ConstantTimeCompare([]byte(candidate), []byte(token)) == 1 {
								next.ServeHTTP(w, r)
								return
							}
						}
					}
				}
				log.Warn().Str("ip", clientIP).Str("path", r.URL.Path).Msg("auth failure: invalid or missing WebSocket auth subprotocol")
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			// API routes: require Authorization: Bearer <token> header.
			auth := r.Header.Get("Authorization")
			if auth == "" {
				log.Warn().Str("ip", clientIP).Str("path", r.URL.Path).Msg("auth failure: missing Authorization header")
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			const prefix = "Bearer "
			if !strings.HasPrefix(auth, prefix) {
				log.Warn().Str("ip", clientIP).Str("path", r.URL.Path).Msg("auth failure: malformed Authorization header")
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			candidate := strings.TrimPrefix(auth, prefix)
			if subtle.ConstantTimeCompare([]byte(candidate), []byte(token)) != 1 {
				log.Warn().Str("ip", clientIP).Str("path", r.URL.Path).Msg("auth failure: invalid token")
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
