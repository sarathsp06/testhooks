package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sarathsp06/testhooks/internal/config"
	"github.com/sarathsp06/testhooks/internal/hub"
)

func newTestCapture(store Store) (*Capture, *hub.Hub) {
	log := zerolog.New(os.Stderr).Level(zerolog.Disabled)
	h := hub.New(100, 300, 50)
	c := NewCapture(store, h, nil, nil, 512*1024, nil, log)
	return c, h
}

// captureMux builds a mux with the capture handler routes.
func captureMux(capture *Capture) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/h/{slug}", capture)
	mux.Handle("/h/{slug}/{rest...}", capture)
	return mux
}

func TestCapture_ServerMode_StoresRequest(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "abc123", "Test", "server", nil)
	capture, h := newTestCapture(store)
	_ = h

	mux := captureMux(capture)

	body := `{"event":"test"}`
	req := httptest.NewRequest("POST", "/h/abc123", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	// Verify response is JSON with status=ok.
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "ok" {
		t.Errorf("response status = %q, want 'ok'", resp["status"])
	}
	if resp["id"] == "" {
		t.Error("response should contain request id")
	}

	// Verify request was stored.
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.requests) != 1 {
		t.Errorf("stored requests = %d, want 1", len(store.requests))
	}
	for _, r := range store.requests {
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.ContentType != "application/json" {
			t.Errorf("content_type = %q, want application/json", r.ContentType)
		}
		if string(r.Body) != body {
			t.Errorf("body = %q, want %q", string(r.Body), body)
		}
	}
}

func TestCapture_ServerMode_SubPath(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "abc123", "Test", "server", nil)
	capture, _ := newTestCapture(store)
	mux := captureMux(capture)

	req := httptest.NewRequest("PUT", "/h/abc123/api/v1/events", strings.NewReader("data"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	for _, r := range store.requests {
		if r.Path != "/api/v1/events" {
			t.Errorf("path = %q, want '/api/v1/events'", r.Path)
		}
		if r.Method != "PUT" {
			t.Errorf("method = %q, want PUT", r.Method)
		}
	}
}

func TestCapture_EndpointNotFound(t *testing.T) {
	store := newMockStore()
	capture, _ := newTestCapture(store)
	mux := captureMux(capture)

	req := httptest.NewRequest("POST", "/h/nonexistent", strings.NewReader("data"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestCapture_BrowserMode_DoesNotStore(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "browser1", "Test", "browser", nil)
	capture, _ := newTestCapture(store)
	mux := captureMux(capture)

	req := httptest.NewRequest("POST", "/h/browser1", strings.NewReader(`{"test":true}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Browser mode returns 202 Accepted.
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}

	// No request should be stored in DB.
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.requests) != 0 {
		t.Errorf("stored requests = %d, want 0 (browser mode)", len(store.requests))
	}
}

func TestCapture_ServerMode_PublishesToHub(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "abc123", "Test", "server", nil)
	capture, h := newTestCapture(store)
	mux := captureMux(capture)

	// Subscribe before sending request.
	ch, cleanup := h.Subscribe("abc123", 10)
	defer cleanup()

	req := httptest.NewRequest("POST", "/h/abc123", strings.NewReader("hello"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	// Should receive a message on the hub.
	select {
	case msg := <-ch:
		if msg.Type != "request" {
			t.Errorf("msg type = %q, want 'request'", msg.Type)
		}
	default:
		t.Error("expected a message on the hub channel")
	}
}

func TestCapture_BrowserMode_PublishesToHub(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "browser1", "Test", "browser", nil)
	capture, h := newTestCapture(store)
	mux := captureMux(capture)

	ch, cleanup := h.Subscribe("browser1", 10)
	defer cleanup()

	req := httptest.NewRequest("POST", "/h/browser1", strings.NewReader("data"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}

	select {
	case msg := <-ch:
		if msg.Type != "request" {
			t.Errorf("msg type = %q, want 'request'", msg.Type)
		}
	default:
		t.Error("expected a message on the hub channel")
	}
}

func TestCapture_DBInsertError(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "abc123", "Test", "server", nil)
	store.insertRequestErr = errTestDB
	capture, _ := newTestCapture(store)
	mux := captureMux(capture)

	req := httptest.NewRequest("POST", "/h/abc123", strings.NewReader("data"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestCapture_ClientIP_XForwardedFor(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "abc123", "Test", "server", nil)
	// Create capture with trusted proxies matching httptest's RemoteAddr (192.0.2.1).
	_, trustedNet, _ := net.ParseCIDR("192.0.2.0/24")
	log := zerolog.New(os.Stderr).Level(zerolog.Disabled)
	h := hub.New(100, 300, 50)
	capture := NewCapture(store, h, nil, nil, 512*1024, []*net.IPNet{trustedNet}, log)
	mux := captureMux(capture)

	req := httptest.NewRequest("POST", "/h/abc123", strings.NewReader("data"))
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	// With trusted proxies, ClientIP walks backwards through XFF and returns
	// the rightmost non-trusted IP. 5.6.7.8 is not in trusted proxies, so it's returned.
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, r := range store.requests {
		if r.IP != "5.6.7.8" {
			t.Errorf("ip = %q, want '5.6.7.8' (rightmost non-trusted from XFF)", r.IP)
		}
	}
}

func TestCapture_QueryParams(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "abc123", "Test", "server", nil)
	capture, _ := newTestCapture(store)
	mux := captureMux(capture)

	req := httptest.NewRequest("GET", "/h/abc123?foo=bar&baz=qux", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	for _, r := range store.requests {
		if r.Query == nil {
			t.Fatal("query should not be nil")
		}
		var q map[string][]string
		json.Unmarshal(r.Query, &q)
		if q["foo"][0] != "bar" {
			t.Errorf("query foo = %q, want 'bar'", q["foo"])
		}
	}
}

func TestCapture_AllMethods(t *testing.T) {
	methods := []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			store := newMockStore()
			store.seedEndpoint("ep-1", "abc123", "Test", "server", nil)
			capture, _ := newTestCapture(store)
			mux := captureMux(capture)

			req := httptest.NewRequest(method, "/h/abc123", strings.NewReader("body"))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want 200 for method %s", w.Code, method)
			}

			store.mu.Lock()
			for _, r := range store.requests {
				if r.Method != method {
					t.Errorf("stored method = %q, want %q", r.Method, method)
				}
			}
			store.mu.Unlock()
		})
	}
}

// errTestDB is a sentinel error for testing DB error injection.
var errTestDB = http.ErrAbortHandler

func TestCapture_DBError_Returns503(t *testing.T) {
	store := newMockStore()
	store.getEndpointBySlugErr = errors.New("db connection lost")
	capture, _ := newTestCapture(store)

	w := httptest.NewRecorder()
	captureMux(capture).ServeHTTP(w, httptest.NewRequest("POST", "/h/abc123", strings.NewReader("{}")))

	// Senders typically retry 5xx but give up on 404, so a DB outage must not look like "not found".
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestCapture_UnknownSlug_Returns404(t *testing.T) {
	capture, _ := newTestCapture(newMockStore())

	w := httptest.NewRecorder()
	captureMux(capture).ServeHTTP(w, httptest.NewRequest("POST", "/h/nope", nil))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

// signStandardWebhook signs body the way Sparrow does (Standard Webhooks v1).
func signStandardWebhook(req *http.Request, rawSecret, id string, body string, at time.Time) {
	ts := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(rawSecret))
	mac.Write([]byte(id + "." + ts + "." + body))
	req.Header.Set("webhook-id", id)
	req.Header.Set("webhook-timestamp", ts)
	req.Header.Set("webhook-signature", "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
}

func TestCapture_Signature(t *testing.T) {
	const secret = "shared-secret"
	body := `{"event":"order.created"}`

	cases := []struct {
		name       string
		enforce    bool
		secret     string // secret used to sign; "" = unsigned
		wantCode   int
		wantStatus string
	}{
		{"valid", true, secret, http.StatusOK, "verified"},
		{"wrong secret enforced", true, "other", http.StatusUnauthorized, "invalid"},
		{"unsigned enforced", true, "", http.StatusUnauthorized, "missing"},
		{"wrong secret report-only", false, "other", http.StatusOK, "invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newMockStore()
			cfg, _ := json.Marshal(map[string]any{"signature": map[string]any{"secret": secret, "enforce": tc.enforce}})
			store.seedEndpoint("ep-1", "sig", "Sig", "server", cfg)
			capture, _ := newTestCapture(store)

			req := httptest.NewRequest("POST", "/h/sig", strings.NewReader(body))
			if tc.secret != "" {
				signStandardWebhook(req, tc.secret, "msg_1", body, time.Now())
			}
			w := httptest.NewRecorder()
			captureMux(capture).ServeHTTP(w, req)

			if w.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d (body %s)", w.Code, tc.wantCode, w.Body)
			}
			store.mu.Lock()
			defer store.mu.Unlock()
			if len(store.requests) != 1 {
				t.Fatalf("stored %d requests, want 1 (rejected attempts are kept)", len(store.requests))
			}
			for _, r := range store.requests {
				if r.Signature == nil || r.Signature.Status != tc.wantStatus {
					t.Errorf("signature = %+v, want status %q", r.Signature, tc.wantStatus)
				}
			}
		})
	}
}

func TestCapture_StripsHeaders(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "abc123", "Test", "server", nil)
	capture, _ := newTestCapture(store)
	mux := captureMux(capture)

	send := func() http.Header {
		store.mu.Lock()
		clear(store.requests)
		store.mu.Unlock()
		req := httptest.NewRequest("POST", "/h/abc123", strings.NewReader("data"))
		req.Header.Set("X-Vercel-Oidc-Token", "jwt")
		req.Header.Set("X-Vercel-Proxy-Signature", "Bearer abc")
		req.Header.Set("X-Vercel-Ip-Country", "US")
		req.Header.Set("X-Internal-Trace", "1")
		req.Header.Set("Webhook-Id", "msg_1")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		store.mu.Lock()
		defer store.mu.Unlock()
		var h http.Header
		for _, r := range store.requests {
			json.Unmarshal(r.Headers, &h)
		}
		return h
	}

	// Defaults remove Vercel's credentials and keep everything else.
	h := send()
	for _, name := range []string{"X-Vercel-Oidc-Token", "X-Vercel-Proxy-Signature"} {
		if h.Get(name) != "" {
			t.Errorf("default: %s was stored", name)
		}
	}
	for _, name := range []string{"X-Vercel-Ip-Country", "X-Internal-Trace", "Webhook-Id"} {
		if h.Get(name) == "" {
			t.Errorf("default: %s was stripped", name)
		}
	}

	// Custom patterns replace the defaults; "*" matches a prefix.
	capture.SetStripHeaders([]string{"x-internal-*", "X-Vercel-Ip-Country"})
	h = send()
	if h.Get("X-Internal-Trace") != "" || h.Get("X-Vercel-Ip-Country") != "" {
		t.Errorf("custom: matching headers stored: %v", h)
	}
	if h.Get("X-Vercel-Oidc-Token") == "" || h.Get("Webhook-Id") == "" {
		t.Errorf("custom: unmatched headers stripped: %v", h)
	}
}

// Browser mode with sync forwarding and no reply script: the server holds the
// sender's request open, asks the tab (response_needed), and relays what the
// tab sends back, which is the forward target's reply, often from localhost.
func TestCapture_BrowserMode_SyncForward_RelaysTabReply(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "browser1", "Test", "browser",
		json.RawMessage(`{"forward_url":"http://localhost:3000/hook","forward_mode":"sync"}`))
	capture, h := newTestCapture(store)
	mux := captureMux(capture)

	ch, cleanup := h.Subscribe("browser1", 10)
	defer cleanup()

	// Play the tab: answer response_needed with the local app's reply.
	go func() {
		for msg := range ch {
			if msg.Type != "response_needed" {
				continue
			}
			var data struct {
				RequestID string `json:"request_id"`
			}
			if err := json.Unmarshal(msg.Data, &data); err != nil {
				return
			}
			h.DeliverResponse("browser1", data.RequestID, hub.ResponseResult{
				RequestID:   data.RequestID,
				Status:      201,
				Headers:     map[string]string{"X-From": "local-app"},
				Body:        `{"received":true}`,
				ContentType: "application/json",
				Source:      "forward_passthrough",
			})
			return
		}
	}()

	req := httptest.NewRequest("POST", "/h/browser1", strings.NewReader(`{"test":true}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 201 {
		t.Fatalf("status = %d, want 201 (the local app's reply)", w.Code)
	}
	if got := w.Body.String(); got != `{"received":true}` {
		t.Errorf("body = %q, want the local app's body", got)
	}
	if got := w.Header().Get("X-From"); got != "local-app" {
		t.Errorf("X-From = %q, want local-app", got)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
}

// Async forwarding in browser mode does not hold the request: 202 as before.
func TestCapture_BrowserMode_AsyncForward_Returns202(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "browser1", "Test", "browser",
		json.RawMessage(`{"forward_url":"http://localhost:3000/hook","forward_mode":"async"}`))
	capture, h := newTestCapture(store)
	mux := captureMux(capture)

	_, cleanup := h.Subscribe("browser1", 10)
	defer cleanup()

	req := httptest.NewRequest("POST", "/h/browser1", strings.NewReader("data"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
}

func TestBrowserDecidesReply(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.EndpointConfig
		want bool
	}{
		{"nothing set", config.EndpointConfig{}, false},
		{"async forward", config.EndpointConfig{ForwardURL: "http://localhost:3000", ForwardMode: "async"}, false},
		{"sync forward", config.EndpointConfig{ForwardURL: "http://localhost:3000", ForwardMode: "sync"}, true},
		{"sync without url", config.EndpointConfig{ForwardMode: "sync"}, false},
		{"reply script", config.EndpointConfig{CustomResponse: &config.CustomResponseConfig{Enabled: true, Script: "function handler(req){return null}"}}, true},
		{"reply script disabled", config.EndpointConfig{CustomResponse: &config.CustomResponseConfig{Enabled: false, Script: "x"}}, false},
		{"legacy lua script", config.EndpointConfig{CustomResponse: &config.CustomResponseConfig{Enabled: true, Script: "x", Language: "lua"}}, false},
	}
	for _, tc := range cases {
		if got := browserDecidesReply(tc.cfg); got != tc.want {
			t.Errorf("%s: browserDecidesReply = %v, want %v", tc.name, got, tc.want)
		}
	}
}
