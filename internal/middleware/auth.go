package middleware

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strings"

	"github.com/rs/zerolog"
)

// TokenAuth returns middleware that validates the token on API (/api/) and
// WebSocket (/ws/) requests. All other paths, including webhook capture (/h/),
// /healthz and the SPA, remain publicly accessible.
// If token is empty, auth is disabled and all requests pass through.
//
// readToken, when set, is a second token that may only read: GET and HEAD on
// the API, and the WebSocket. A write with it gets 403, not 401, so a client
// knows the token is right but not enough.
//
// Auth failures are logged at Warn level with the client IP for auditing.
func TokenAuth(token, readToken string, log zerolog.Logger, trustedProxies []*net.IPNet) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Auth disabled if no token configured.
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Only the API and WebSocket carry data, so only they require the
			// token. Everything else stays public: webhook capture (/h/), which
			// external senders can't authenticate to; /healthz, for orchestrator
			// probes; and the SPA (/, /_app/*, client-side routes like /<slug>),
			// whose bundle holds no data and must load to show the sign-in prompt.
			isWS := strings.HasPrefix(r.URL.Path, "/ws/")
			if !isWS && !strings.HasPrefix(r.URL.Path, "/api/") {
				next.ServeHTTP(w, r)
				return
			}
			// The API description holds no data and tells a client how to
			// authenticate, so it stays public like the UI bundle.
			if r.URL.Path == "/api/openapi.json" {
				next.ServeHTTP(w, r)
				return
			}

			clientIP := ClientIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"), r.Header.Get("X-Real-Ip"), trustedProxies)

			// WebSocket upgrade requests pass token via Sec-WebSocket-Protocol header
			// (browsers can't set Authorization on WS). The subprotocol format is
			// "auth.<token>" — the server selects it to echo back.
			if isWS {
				protocols := r.Header.Get("Sec-WebSocket-Protocol")
				if protocols != "" {
					for _, proto := range strings.Split(protocols, ",") {
						proto = strings.TrimSpace(proto)
						if strings.HasPrefix(proto, "auth.") {
							candidate := strings.TrimPrefix(proto, "auth.")
							if matches(candidate, token) || matches(candidate, readToken) {
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
			switch {
			case matches(candidate, token):
				next.ServeHTTP(w, r)
			case matches(candidate, readToken):
				if r.Method == http.MethodGet || r.Method == http.MethodHead {
					next.ServeHTTP(w, r)
					return
				}
				log.Warn().Str("ip", clientIP).Str("path", r.URL.Path).Str("method", r.Method).Msg("auth failure: read-only token used for a write")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"this token can only read; changing or sending anything needs AUTH_TOKEN"}`))
			default:
				log.Warn().Str("ip", clientIP).Str("path", r.URL.Path).Msg("auth failure: invalid token")
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
			}
		})
	}
}

// matches compares a presented token with a configured one in constant time.
// An empty configured token never matches.
func matches(candidate, token string) bool {
	return token != "" && subtle.ConstantTimeCompare([]byte(candidate), []byte(token)) == 1
}
