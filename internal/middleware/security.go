package middleware

import "net/http"

// SecurityHeaders returns an HTTP middleware that sets security-related headers
// on every response. These headers provide defense-in-depth against common
// web vulnerabilities (XSS, clickjacking, MIME sniffing, etc.).
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()

		// Prevent MIME type sniffing — forces browser to respect declared Content-Type.
		h.Set("X-Content-Type-Options", "nosniff")

		// Prevent the page from being embedded in iframes (clickjacking protection).
		h.Set("X-Frame-Options", "DENY")

		// Control referrer information sent with requests.
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Content Security Policy — restrictive but allows the SPA to function:
		// - default-src 'self': only load resources from same origin
		// - script-src 'self' 'unsafe-inline' 'wasm-unsafe-eval': SvelteKit emits an inline
		//   bootstrap <script> in index.html; 'wasm-unsafe-eval' for QuickJS WASM
		// - style-src 'self' 'unsafe-inline' https://fonts.googleapis.com: Tailwind/SvelteKit
		//   inline styles + Google Fonts stylesheets
		// - connect-src 'self' ws: wss: https: http:: allow API calls, WebSocket, and
		//   browser-side forwarding to arbitrary targets (including localhost)
		// - img-src 'self' data:: allow same-origin images + data URIs
		// - font-src 'self' https://fonts.gstatic.com: same-origin + Google Fonts files
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline' 'wasm-unsafe-eval'; "+
				"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; "+
				"connect-src 'self' ws: wss: https: http:; "+
				"img-src 'self' data:; "+
				"font-src 'self' https://fonts.gstatic.com")

		next.ServeHTTP(w, r)
	})
}
