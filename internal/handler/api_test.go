package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/sarathsp06/testhooks/internal/db"
)

func newTestAPI(store Store) *API {
	log := zerolog.New(os.Stderr).Level(zerolog.Disabled)
	return NewAPI(store, nil, log)
}

// newMux creates an http.ServeMux wired with all API routes for test purposes.
func newMux(api *API) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/endpoints", api.CreateEndpoint)
	mux.HandleFunc("GET /api/endpoints", api.ListEndpoints)
	mux.HandleFunc("GET /api/endpoints/{id}", api.GetEndpoint)
	mux.HandleFunc("PATCH /api/endpoints/{id}", api.UpdateEndpoint)
	mux.HandleFunc("DELETE /api/endpoints/{id}", api.DeleteEndpoint)
	mux.HandleFunc("GET /api/endpoints/{id}/requests", api.ListRequests)
	mux.HandleFunc("DELETE /api/endpoints/{id}/requests", api.DeleteAllRequests)
	mux.HandleFunc("GET /api/requests/{reqId}", api.GetRequest)
	mux.HandleFunc("DELETE /api/requests/{reqId}", api.DeleteRequest)
	return mux
}

func TestAPI_CreateEndpoint_Default(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	mux := newMux(api)

	req := httptest.NewRequest("POST", "/api/endpoints", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}

	var ep db.Endpoint
	if err := json.NewDecoder(w.Body).Decode(&ep); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ep.Mode != "server" {
		t.Errorf("mode = %q, want 'server'", ep.Mode)
	}
	if ep.Slug == "" {
		t.Error("slug should not be empty")
	}
}

func TestAPI_CreateEndpoint_BrowserMode(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	mux := newMux(api)

	req := httptest.NewRequest("POST", "/api/endpoints", strings.NewReader(`{"mode":"browser","name":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}
	var ep db.Endpoint
	json.NewDecoder(w.Body).Decode(&ep)
	if ep.Mode != "browser" {
		t.Errorf("mode = %q, want 'browser'", ep.Mode)
	}
	if ep.Name != "test" {
		t.Errorf("name = %q, want 'test'", ep.Name)
	}
}

func TestAPI_CreateEndpoint_InvalidMode(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	mux := newMux(api)

	req := httptest.NewRequest("POST", "/api/endpoints", strings.NewReader(`{"mode":"invalid"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestAPI_CreateEndpoint_DBError(t *testing.T) {
	store := newMockStore()
	store.createEndpointErr = fmt.Errorf("db down")
	api := newTestAPI(store)
	mux := newMux(api)

	req := httptest.NewRequest("POST", "/api/endpoints", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestAPI_ListEndpoints(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "abc123", "Endpoint 1", "server", nil)
	store.seedEndpoint("ep-2", "def456", "Endpoint 2", "browser", nil)
	api := newTestAPI(store)
	mux := newMux(api)

	req := httptest.NewRequest("GET", "/api/endpoints", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var endpoints []db.Endpoint
	json.NewDecoder(w.Body).Decode(&endpoints)
	if len(endpoints) != 2 {
		t.Errorf("got %d endpoints, want 2", len(endpoints))
	}
}

func TestAPI_ListEndpoints_Empty(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	mux := newMux(api)

	req := httptest.NewRequest("GET", "/api/endpoints", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	// Should return empty array, not null.
	body := strings.TrimSpace(w.Body.String())
	if body != "[]" {
		t.Errorf("body = %q, want '[]'", body)
	}
}

func TestAPI_GetEndpoint(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "abc123", "Test", "server", nil)
	api := newTestAPI(store)
	mux := newMux(api)

	req := httptest.NewRequest("GET", "/api/endpoints/ep-1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var ep db.Endpoint
	json.NewDecoder(w.Body).Decode(&ep)
	if ep.ID != "ep-1" {
		t.Errorf("id = %q, want 'ep-1'", ep.ID)
	}
}

func TestAPI_GetEndpoint_NotFound(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	mux := newMux(api)

	req := httptest.NewRequest("GET", "/api/endpoints/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestAPI_UpdateEndpoint(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "abc123", "Old Name", "server", nil)
	api := newTestAPI(store)
	mux := newMux(api)

	req := httptest.NewRequest("PATCH", "/api/endpoints/ep-1",
		strings.NewReader(`{"name":"New Name","mode":"browser"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var ep db.Endpoint
	json.NewDecoder(w.Body).Decode(&ep)
	if ep.Name != "New Name" {
		t.Errorf("name = %q, want 'New Name'", ep.Name)
	}
	if ep.Mode != "browser" {
		t.Errorf("mode = %q, want 'browser'", ep.Mode)
	}
}

func TestAPI_UpdateEndpoint_NotFound(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	mux := newMux(api)

	req := httptest.NewRequest("PATCH", "/api/endpoints/nonexistent",
		strings.NewReader(`{"name":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestAPI_UpdateEndpoint_InvalidMode(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "abc123", "Test", "server", nil)
	api := newTestAPI(store)
	mux := newMux(api)

	req := httptest.NewRequest("PATCH", "/api/endpoints/ep-1",
		strings.NewReader(`{"mode":"invalid"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestAPI_DeleteEndpoint(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "abc123", "Test", "server", nil)
	api := newTestAPI(store)
	mux := newMux(api)

	req := httptest.NewRequest("DELETE", "/api/endpoints/ep-1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}

	// Verify it's gone.
	store.mu.Lock()
	_, exists := store.endpoints["ep-1"]
	store.mu.Unlock()
	if exists {
		t.Error("endpoint should have been deleted")
	}
}

func TestAPI_ListRequests(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "abc123", "Test", "server", nil)
	store.seedRequest("req-1", "ep-1", "POST", "/webhook")
	store.seedRequest("req-2", "ep-1", "GET", "/ping")
	api := newTestAPI(store)
	mux := newMux(api)

	req := httptest.NewRequest("GET", "/api/endpoints/ep-1/requests", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var result struct {
		Requests []db.CapturedRequest `json:"requests"`
		Total    int                  `json:"total"`
		Limit    int                  `json:"limit"`
		Offset   int                  `json:"offset"`
	}
	json.NewDecoder(w.Body).Decode(&result)
	if result.Total != 2 {
		t.Errorf("total = %d, want 2", result.Total)
	}
	if len(result.Requests) != 2 {
		t.Errorf("requests len = %d, want 2", len(result.Requests))
	}
}

func TestAPI_ListRequests_EndpointNotFound(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	mux := newMux(api)

	req := httptest.NewRequest("GET", "/api/endpoints/nonexistent/requests", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestAPI_ListRequests_WithPagination(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "abc123", "Test", "server", nil)
	for i := 0; i < 5; i++ {
		store.seedRequest(fmt.Sprintf("req-%d", i), "ep-1", "POST", "/webhook")
	}
	api := newTestAPI(store)
	mux := newMux(api)

	req := httptest.NewRequest("GET", "/api/endpoints/ep-1/requests?limit=2&offset=1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var result struct {
		Requests []db.CapturedRequest `json:"requests"`
		Total    int                  `json:"total"`
		Limit    int                  `json:"limit"`
		Offset   int                  `json:"offset"`
	}
	json.NewDecoder(w.Body).Decode(&result)
	if result.Limit != 2 {
		t.Errorf("limit = %d, want 2", result.Limit)
	}
	if result.Offset != 1 {
		t.Errorf("offset = %d, want 1", result.Offset)
	}
	if len(result.Requests) != 2 {
		t.Errorf("requests len = %d, want 2", len(result.Requests))
	}
}

func TestAPI_GetRequest(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "abc123", "Test", "server", nil)
	store.seedRequest("req-1", "ep-1", "POST", "/webhook")
	api := newTestAPI(store)
	mux := newMux(api)

	req := httptest.NewRequest("GET", "/api/requests/req-1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var captured db.CapturedRequest
	json.NewDecoder(w.Body).Decode(&captured)
	if captured.ID != "req-1" {
		t.Errorf("id = %q, want 'req-1'", captured.ID)
	}
}

func TestAPI_GetRequest_NotFound(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	mux := newMux(api)

	req := httptest.NewRequest("GET", "/api/requests/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestAPI_DeleteRequest(t *testing.T) {
	store := newMockStore()
	store.seedRequest("req-1", "ep-1", "POST", "/webhook")
	api := newTestAPI(store)
	mux := newMux(api)

	req := httptest.NewRequest("DELETE", "/api/requests/req-1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}

	store.mu.Lock()
	_, exists := store.requests["req-1"]
	store.mu.Unlock()
	if exists {
		t.Error("request should have been deleted")
	}
}

func TestAPI_DeleteAllRequests(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "abc123", "Test", "server", nil)
	store.seedRequest("req-1", "ep-1", "POST", "/a")
	store.seedRequest("req-2", "ep-1", "POST", "/b")
	store.seedRequest("req-3", "ep-2", "POST", "/c") // Different endpoint, should not be deleted.
	api := newTestAPI(store)
	mux := newMux(api)

	req := httptest.NewRequest("DELETE", "/api/endpoints/ep-1/requests", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	// ep-1 requests should be gone.
	for _, r := range store.requests {
		if r.EndpointID == "ep-1" {
			t.Error("ep-1 requests should have been deleted")
		}
	}
	// ep-2 request should still exist.
	if _, ok := store.requests["req-3"]; !ok {
		t.Error("ep-2 request should not have been deleted")
	}
}

func TestAPI_CreateEndpoint_EmptyBody(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	mux := newMux(api)

	// Empty body — should default to server mode.
	req := httptest.NewRequest("POST", "/api/endpoints", strings.NewReader(""))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}
	var ep db.Endpoint
	json.NewDecoder(w.Body).Decode(&ep)
	if ep.Mode != "server" {
		t.Errorf("mode = %q, want 'server'", ep.Mode)
	}
}

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusOK, map[string]string{"hello": "world"})

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q, want application/json", ct)
	}
}

func TestGenerateSlug(t *testing.T) {
	slug1 := GenerateSlug()
	slug2 := GenerateSlug()

	if len(slug1) != 8 {
		t.Errorf("slug length = %d, want 8", len(slug1))
	}
	if slug1 == slug2 {
		t.Error("two slugs should not be equal")
	}
}
