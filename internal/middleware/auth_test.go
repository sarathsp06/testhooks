package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
)

func TestTokenAuth(t *testing.T) {
	const token = "s3cret"
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := TokenAuth(token, "", zerolog.Nop(), nil)(ok)

	cases := []struct {
		name   string
		path   string
		header map[string]string
		want   int
	}{
		// Public: capture, health, and every SPA path (the bundle must load to show the sign-in prompt).
		{"capture", "/h/abc123", nil, 200},
		{"capture subpath", "/h/abc123/stripe/events", nil, 200},
		{"healthz", "/healthz", nil, 200},
		{"spa root", "/", nil, 200},
		{"spa bundle", "/_app/immutable/entry/start.js", nil, 200},
		{"spa client route", "/abc123", nil, 200},

		// Protected: API.
		{"api no token", "/api/endpoints", nil, 401},
		{"api wrong token", "/api/endpoints", map[string]string{"Authorization": "Bearer nope"}, 401},
		{"api not bearer", "/api/endpoints", map[string]string{"Authorization": "Basic " + token}, 401},
		{"api good token", "/api/endpoints", map[string]string{"Authorization": "Bearer " + token}, 200},

		// Protected: WebSocket, token passed as subprotocol.
		{"ws no token", "/ws/abc123", nil, 401},
		{"ws wrong token", "/ws/abc123", map[string]string{"Sec-WebSocket-Protocol": "auth.nope"}, 401},
		{"ws good token", "/ws/abc123", map[string]string{"Sec-WebSocket-Protocol": "other, auth." + token}, 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.path, nil)
			for k, v := range tc.header {
				req.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Errorf("GET %s = %d, want %d", tc.path, w.Code, tc.want)
			}
		})
	}
}

func TestTokenAuth_Disabled(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := TokenAuth("", "", zerolog.Nop(), nil)(ok)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/endpoints", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("auth disabled: got %d, want 200", w.Code)
	}
}

func TestTokenAuth_ReadToken(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := TokenAuth("full", "readonly", zerolog.Nop(), nil)(ok)

	cases := []struct {
		name, method, path string
		header             map[string]string
		want               int
	}{
		{"read token reads", "GET", "/api/endpoints", map[string]string{"Authorization": "Bearer readonly"}, 200},
		{"read token heads", "HEAD", "/api/endpoints", map[string]string{"Authorization": "Bearer readonly"}, 200},
		{"read token waits", "GET", "/api/endpoints/x/requests?wait=5", map[string]string{"Authorization": "Bearer readonly"}, 200},
		{"read token cannot write", "POST", "/api/endpoints", map[string]string{"Authorization": "Bearer readonly"}, 403},
		{"read token cannot delete", "DELETE", "/api/endpoints/x", map[string]string{"Authorization": "Bearer readonly"}, 403},
		{"read token cannot preview", "POST", "/api/endpoints/x/preview", map[string]string{"Authorization": "Bearer readonly"}, 403},
		{"read token on ws", "GET", "/ws/abc", map[string]string{"Sec-WebSocket-Protocol": "auth.readonly"}, 200},
		{"full token writes", "POST", "/api/endpoints", map[string]string{"Authorization": "Bearer full"}, 200},
		{"wrong token", "GET", "/api/endpoints", map[string]string{"Authorization": "Bearer nope"}, 401},
		{"openapi is public", "GET", "/api/openapi.json", nil, 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			for k, v := range tc.header {
				req.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Errorf("%s %s = %d, want %d", tc.method, tc.path, w.Code, tc.want)
			}
		})
	}

	// An empty read token never matches, so an empty bearer is still 401.
	h = TokenAuth("full", "", zerolog.Nop(), nil)(ok)
	req := httptest.NewRequest("GET", "/api/endpoints", nil)
	req.Header.Set("Authorization", "Bearer ")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("empty bearer = %d, want 401", w.Code)
	}
}
