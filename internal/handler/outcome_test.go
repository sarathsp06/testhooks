package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/sarathsp06/testhooks/internal/db"
	"github.com/sarathsp06/testhooks/internal/forward"
)

// forwardingCapture returns a capture handler that forwards to target and the
// store behind it, with one server-mode endpoint "abc123".
func forwardingCapture(t *testing.T, target, mode string) (*Capture, *mockStore) {
	t.Helper()
	store := newMockStore()
	cfg, _ := json.Marshal(map[string]any{"forward_url": target, "forward_mode": mode})
	store.seedEndpoint("ep-1", "abc123", "Test", "server", cfg)
	capture, _ := newTestCapture(store)
	log := zerolog.New(os.Stderr).Level(zerolog.Disabled)
	capture.forwarder = forward.New(forward.Config{DisableSSRFProtection: true}, log)
	return capture, store
}

func onlyRequest(t *testing.T, store *mockStore) *db.CapturedRequest {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.requests) != 1 {
		t.Fatalf("stored %d requests, want 1", len(store.requests))
	}
	for _, r := range store.requests {
		c := *r
		return &c
	}
	return nil
}

func TestCapture_RecordsForwardResultAndResponse(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprint(w, `{"received":true}`)
	}))
	defer target.Close()

	for _, mode := range []string{"sync", "async"} {
		t.Run(mode, func(t *testing.T) {
			capture, store := forwardingCapture(t, target.URL, mode)
			w := httptest.NewRecorder()
			captureMux(capture).ServeHTTP(w, httptest.NewRequest("POST", "/h/abc123", strings.NewReader(`{"a":1}`)))
			// Async forwards finish in the background.
			if err := capture.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}

			req := onlyRequest(t, store)
			var fwd forwardResultPayload
			if err := json.Unmarshal(req.ForwardResult, &fwd); err != nil {
				t.Fatalf("forward_result %q: %v", req.ForwardResult, err)
			}
			if !fwd.OK || fwd.StatusCode != http.StatusAccepted || fwd.ResponseBody != `{"received":true}` || fwd.RequestID != req.ID {
				t.Errorf("forward_result = %+v", fwd)
			}

			var resp responseInfoPayload
			if err := json.Unmarshal(req.ResponseInfo, &resp); err != nil {
				t.Fatalf("response_info %q: %v", req.ResponseInfo, err)
			}
			wantSource := map[string]string{"sync": "forward_passthrough", "async": "default"}[mode]
			if resp.Status != w.Code || resp.Source != wantSource {
				t.Errorf("response_info = %+v, want status %d from %s", resp, w.Code, wantSource)
			}

			// The API returns both with the request.
			body := getJSON(t, "/api/requests/"+req.ID, store)
			if _, ok := body["forward_result"]; !ok {
				t.Error("GET /api/requests/{id} has no forward_result")
			}
			if _, ok := body["response_info"]; !ok {
				t.Error("GET /api/requests/{id} has no response_info")
			}
		})
	}
}

func TestCapture_RecordsForwardFailure(t *testing.T) {
	capture, store := forwardingCapture(t, "http://127.0.0.1:1", "sync")
	capture.forwarder = forward.New(forward.Config{DisableSSRFProtection: true, Timeout: 1e9}, zerolog.Nop())
	captureMux(capture).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/h/abc123", strings.NewReader("x")))

	var fwd forwardResultPayload
	json.Unmarshal(onlyRequest(t, store).ForwardResult, &fwd)
	if fwd.OK || fwd.Error == "" {
		t.Errorf("forward_result = %+v, want a failure with an error", fwd)
	}
}

func TestCapture_TruncatesStoredReplyBody(t *testing.T) {
	const limit = 1000
	big := strings.Repeat("x", limit+10)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, big)
	}))
	defer target.Close()

	capture, store := forwardingCapture(t, target.URL, "sync")
	if capture.maxStoredResponse != 64<<10 {
		t.Errorf("default limit = %d, want 64KB", capture.maxStoredResponse)
	}
	capture.SetMaxStoredResponse(limit)
	captureMux(capture).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/h/abc123", strings.NewReader("x")))

	req := onlyRequest(t, store)
	var fwd forwardResultPayload
	json.Unmarshal(req.ForwardResult, &fwd)
	if len(fwd.ResponseBody) != limit || !fwd.ResponseBodyTruncated {
		t.Errorf("stored body %d bytes (truncated=%v), want %d and truncated", len(fwd.ResponseBody), fwd.ResponseBodyTruncated, limit)
	}
	var resp responseInfoPayload
	json.Unmarshal(req.ResponseInfo, &resp)
	if !resp.BodyTruncated {
		t.Error("passthrough response body not marked truncated")
	}
}

func getJSON(t *testing.T, path string, store Store) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	newMux(newTestAPI(store)).ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s = %d", path, w.Code)
	}
	var out map[string]any
	json.Unmarshal(w.Body.Bytes(), &out)
	return out
}
