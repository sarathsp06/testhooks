package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Headers the hosting platform adds are stored, but left out of what the API
// shows and of what is forwarded.
func TestCapture_HidesPlatformHeaders(t *testing.T) {
	var mu sync.Mutex
	var forwarded http.Header
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		forwarded = r.Header.Clone()
		mu.Unlock()
	}))
	defer target.Close()

	capture, store := forwardingCapture(t, target.URL, "sync")
	hide := []string{"X-Vercel-*", "X-Forwarded-*"}
	capture.SetHideHeaders(hide)
	req := httptest.NewRequest("POST", "/h/abc123", strings.NewReader(`{}`))
	req.Header.Set("X-Vercel-Id", "fra1::abc")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("Webhook-Id", "msg_1")
	captureMux(capture).ServeHTTP(httptest.NewRecorder(), req)

	// Stored in full.
	stored := onlyRequest(t, store)
	var h http.Header
	json.Unmarshal(stored.Headers, &h)
	if h.Get("X-Vercel-Id") == "" || h.Get("X-Forwarded-Proto") == "" || h.Get("Webhook-Id") == "" {
		t.Errorf("stored headers = %v, want all three", h)
	}

	// Not forwarded.
	mu.Lock()
	if forwarded.Get("X-Vercel-Id") != "" || forwarded.Get("X-Forwarded-Proto") != "" || forwarded.Get("Webhook-Id") == "" {
		t.Errorf("forwarded headers = %v, want only the sender's", forwarded)
	}
	mu.Unlock()

	// Hidden from the API unless asked for.
	api := newTestAPI(store)
	api.SetHideHeaders(hide)
	get := func(path string) map[string]any {
		w := httptest.NewRecorder()
		newMux(api).ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	shown := get("/api/requests/" + stored.ID)
	headers := shown["headers"].(map[string]any)
	if _, ok := headers["X-Vercel-Id"]; ok {
		t.Errorf("API shows hidden header: %v", headers)
	}
	if _, ok := headers["Webhook-Id"]; !ok {
		t.Errorf("API hides sender header: %v", headers)
	}
	if shown["hidden_headers"] != float64(2) {
		t.Errorf("hidden_headers = %v, want 2", shown["hidden_headers"])
	}
	list := get("/api/endpoints/ep-1/requests")
	first := list["requests"].([]any)[0].(map[string]any)
	if _, ok := first["headers"].(map[string]any)["X-Forwarded-Proto"]; ok {
		t.Error("list shows hidden header")
	}
	all := get("/api/requests/" + stored.ID + "?include_hidden=true")
	if _, ok := all["headers"].(map[string]any)["X-Vercel-Id"]; !ok {
		t.Error("include_hidden=true still hides the header")
	}
}

// On a function host an async forward finishes before the reply, so the
// result is recorded by the time the sender has its answer.
func TestCapture_Serverless_AsyncForwardFinishesBeforeReply(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer target.Close()

	capture, store := forwardingCapture(t, target.URL, "async")
	capture.SetServerless(true)
	w := httptest.NewRecorder()
	captureMux(capture).ServeHTTP(w, httptest.NewRequest("POST", "/h/abc123", strings.NewReader("x")))

	// No Shutdown wait: the result must already be there.
	var fwd forwardResultPayload
	json.Unmarshal(onlyRequest(t, store).ForwardResult, &fwd)
	if !fwd.OK || fwd.StatusCode != http.StatusAccepted || fwd.InProgress {
		t.Errorf("forward_result = %+v, want a finished 202", fwd)
	}
	// The sender still gets the default reply, not the target's.
	if w.Code != http.StatusOK {
		t.Errorf("reply = %d, want the default 200", w.Code)
	}
}

// A background forward records that it started, so one that never finishes
// shows as such.
func TestCapture_AsyncForward_RecordsStart(t *testing.T) {
	release := make(chan struct{})
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer target.Close()

	capture, store := forwardingCapture(t, target.URL, "async")
	captureMux(capture).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/h/abc123", strings.NewReader("x")))

	var fwd forwardResultPayload
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		json.Unmarshal(onlyRequest(t, store).ForwardResult, &fwd)
		if fwd.InProgress {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !fwd.InProgress || fwd.URL != target.URL {
		t.Errorf("forward_result while running = %+v, want in_progress", fwd)
	}

	close(release)
	capture.Shutdown(context.Background())
	fwd = forwardResultPayload{}
	json.Unmarshal(onlyRequest(t, store).ForwardResult, &fwd)
	if fwd.InProgress || !fwd.OK {
		t.Errorf("forward_result after = %+v, want finished", fwd)
	}
}
