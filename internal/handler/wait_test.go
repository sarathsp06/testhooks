package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sarathsp06/testhooks/internal/db"
	"github.com/sarathsp06/testhooks/internal/hub"
)

type listReply struct {
	Requests []json.RawMessage `json:"requests"`
	Total    int               `json:"total"`
}

func listRequests(t *testing.T, mux *http.ServeMux, path string) (int, listReply, string) {
	t.Helper()
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	var out listReply
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out, w.Body.String()
}

func ids(reqs []json.RawMessage) []string {
	var out []string
	for _, r := range reqs {
		var h struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(r, &h)
		out = append(out, h.ID)
	}
	return out
}

func TestListRequests_After(t *testing.T) {
	store := newMockStore()
	ep := store.seedEndpoint("ep-1", "abc", "test", "server", json.RawMessage(`{}`))
	mux := newMux(newTestAPI(store))
	for range 3 {
		_ = store.InsertRequest(t.Context(), &db.CapturedRequest{EndpointID: ep.ID, Method: "POST", Path: "/"})
		time.Sleep(2 * time.Millisecond) // distinct created_at
	}

	code, out, _ := listRequests(t, mux, "/api/endpoints/ep-1/requests?after=req-1")
	if code != 200 || strings.Join(ids(out.Requests), ",") != "req-2,req-3" || out.Total != 3 {
		t.Fatalf("got %d %v total %d; want req-2,req-3 of 3", code, ids(out.Requests), out.Total)
	}
	code, _, body := listRequests(t, mux, "/api/endpoints/ep-1/requests?after=nope")
	if code != 404 || !strings.Contains(body, "without after") {
		t.Fatalf("unknown after: got %d %s", code, body)
	}
	store.seedEndpoint("ep-2", "def", "test", "server", json.RawMessage(`{}`))
	code, _, body = listRequests(t, mux, "/api/endpoints/ep-2/requests?after=req-1")
	if code != 400 || !strings.Contains(body, "this endpoint") {
		t.Fatalf("after from another endpoint: got %d %s", code, body)
	}
	code, _, body = listRequests(t, mux, "/api/endpoints/ep-1/requests?wait=x")
	if code != 400 || !strings.Contains(body, "0 to 60") {
		t.Fatalf("bad wait: got %d %s", code, body)
	}
}

func TestListRequests_WaitReturnsWhenRequestArrives(t *testing.T) {
	store := newMockStore()
	ep := store.seedEndpoint("ep-1", "abc", "test", "server", json.RawMessage(`{}`))
	h := hub.New(10, 60, 5)
	defer h.Stop()
	api := NewAPI(store, h, zerolog.New(os.Stderr).Level(zerolog.Disabled))
	mux := newMux(api)
	_ = store.InsertRequest(t.Context(), &db.CapturedRequest{EndpointID: ep.ID, Method: "POST", Path: "/old"})

	// Capture a request a little after the wait starts, as the capture handler
	// does: store it, then publish the event.
	go func() {
		time.Sleep(50 * time.Millisecond)
		r := &db.CapturedRequest{EndpointID: ep.ID, Method: "POST", Path: "/new"}
		_ = store.InsertRequest(t.Context(), r)
		data, _ := json.Marshal(r)
		h.Publish(ep.Slug, hub.Message{Type: "request", Data: data}, false)
	}()

	started := time.Now()
	code, out, body := listRequests(t, mux, "/api/endpoints/ep-1/requests?after=req-1&wait=10")
	if code != 200 || strings.Join(ids(out.Requests), ",") != "req-2" {
		t.Fatalf("got %d %s", code, body)
	}
	if time.Since(started) > 5*time.Second {
		t.Fatal("waited for the timeout instead of the event")
	}
}

func TestListRequests_WaitTimesOutEmpty(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "abc", "test", "server", json.RawMessage(`{}`))
	h := hub.New(10, 60, 5)
	defer h.Stop()
	mux := newMux(NewAPI(store, h, zerolog.New(os.Stderr).Level(zerolog.Disabled)))

	started := time.Now()
	code, out, body := listRequests(t, mux, "/api/endpoints/ep-1/requests?wait=1")
	if code != 200 || len(out.Requests) != 0 || !strings.Contains(body, `"requests":[]`) {
		t.Fatalf("got %d %s", code, body)
	}
	if d := time.Since(started); d < 900*time.Millisecond {
		t.Fatalf("returned after %v, want about 1s", d)
	}
}

func TestListRequests_WaitBrowserModeTakesRequestFromEvent(t *testing.T) {
	store := newMockStore()
	ep := store.seedEndpoint("ep-1", "abc", "test", "browser", json.RawMessage(`{}`))
	h := hub.New(10, 60, 5)
	defer h.Stop()
	mux := newMux(NewAPI(store, h, zerolog.New(os.Stderr).Level(zerolog.Disabled)))

	go func() {
		time.Sleep(50 * time.Millisecond)
		// Browser-mode requests are not stored; only the event carries them.
		h.Publish(ep.Slug, hub.Message{Type: "request", Data: json.RawMessage(`{"id":"live-1","endpoint_id":"ep-1","method":"POST","created_at":"2026-11-10T10:00:00Z","body":"hi","body_encoding":"text"}`)}, true)
	}()
	code, out, body := listRequests(t, mux, "/api/endpoints/ep-1/requests?wait=10")
	if code != 200 || strings.Join(ids(out.Requests), ",") != "live-1" || !strings.Contains(body, `"body":"hi"`) {
		t.Fatalf("got %d %s", code, body)
	}
}
