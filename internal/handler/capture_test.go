package handler

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/rs/zerolog"
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
