package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/sarathsp06/testhooks/internal/db"
)

type fakeHub struct{ removed []string }

func (h *fakeHub) RemoveBuffer(slug string) { h.removed = append(h.removed, slug) }

func doEndpoint(t *testing.T, mux *http.ServeMux, method, path, body string) (*httptest.ResponseRecorder, db.Endpoint) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var ep db.Endpoint
	json.Unmarshal(w.Body.Bytes(), &ep)
	return w, ep
}

// threeWords reports whether s is three dash-separated lowercase words.
func threeWords(s string) bool {
	parts := strings.Split(s, "-")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" || strings.Trim(p, "abcdefghijklmnopqrstuvwxyz") != "" {
			return false
		}
	}
	return true
}

func TestAPI_CreateEndpoint_Slug(t *testing.T) {
	mux := newMux(newTestAPI(newMockStore()))

	w, ep := doEndpoint(t, mux, "POST", "/api/endpoints", `{}`)
	if w.Code != http.StatusCreated || !threeWords(ep.Slug) {
		t.Errorf("no name: status %d, slug %q; want 201 and three words", w.Code, ep.Slug)
	}

	w, ep = doEndpoint(t, mux, "POST", "/api/endpoints", `{"slug_name":"Stripe-Test"}`)
	if w.Code != http.StatusCreated || !strings.HasPrefix(ep.Slug, "stripe-test-") || !threeWords(strings.TrimPrefix(ep.Slug, "stripe-test-")) {
		t.Errorf("with name: status %d, slug %q; want 201 and stripe-test-word-word-word", w.Code, ep.Slug)
	}

	w, _ = doEndpoint(t, mux, "POST", "/api/endpoints", `{"slug_name":"api/../x"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "lowercase letters") {
		t.Errorf("bad name: status %d, body %s; want 400 with a plain message", w.Code, w.Body)
	}
}

func TestAPI_CreateEndpoint_RetriesTakenSlug(t *testing.T) {
	store := newMockStore()
	store.slugTakenTimes = 2
	mux := newMux(newTestAPI(store))

	w, ep := doEndpoint(t, mux, "POST", "/api/endpoints", `{"slug_name":"shop"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 after retries; body %s", w.Code, w.Body)
	}
	if len(store.triedSlugs) != 3 || store.triedSlugs[2] != ep.Slug {
		t.Errorf("tried %v, want 3 slugs ending with %q", store.triedSlugs, ep.Slug)
	}
}

func TestAPI_CreateEndpoint_AllSlugsTaken(t *testing.T) {
	store := newMockStore()
	store.slugTakenTimes = 100
	mux := newMux(newTestAPI(store))

	w, _ := doEndpoint(t, mux, "POST", "/api/endpoints", `{}`)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}
	if len(store.triedSlugs) != slugAttempts {
		t.Errorf("tried %d slugs, want %d", len(store.triedSlugs), slugAttempts)
	}
}

func TestAPI_RenameEndpoint(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "abc12345", "Shop", "server", nil)
	hub := &fakeHub{}
	api := NewAPI(store, hub, zerolog.New(os.Stderr).Level(zerolog.Disabled))
	mux := newMux(api)

	w, ep := doEndpoint(t, mux, "PATCH", "/api/endpoints/ep-1", `{"slug_name":"orders"}`)
	if w.Code != http.StatusOK || !strings.HasPrefix(ep.Slug, "orders-") || !threeWords(strings.TrimPrefix(ep.Slug, "orders-")) {
		t.Fatalf("rename: status %d, slug %q; want 200 and orders-word-word-word", w.Code, ep.Slug)
	}
	if ep.Name != "Shop" || ep.Mode != "server" {
		t.Errorf("rename changed other fields: %+v", ep)
	}
	if len(hub.removed) != 1 || hub.removed[0] != "abc12345" {
		t.Errorf("removed buffers %v, want the old slug", hub.removed)
	}
	if _, err := store.GetEndpointBySlug(t.Context(), "abc12345"); err == nil {
		t.Error("old slug still resolves")
	}

	// An empty name drops the name: three new words only.
	_, ep = doEndpoint(t, mux, "PATCH", "/api/endpoints/ep-1", `{"slug_name":""}`)
	if !threeWords(ep.Slug) {
		t.Errorf("rename without a name: slug %q, want three words", ep.Slug)
	}

	// Leaving slug_name out keeps the slug.
	before := ep.Slug
	_, ep = doEndpoint(t, mux, "PATCH", "/api/endpoints/ep-1", `{"name":"Shop 2"}`)
	if ep.Slug != before {
		t.Errorf("slug changed from %q to %q without slug_name", before, ep.Slug)
	}
}

func TestAPI_RenameEndpoint_Invalid(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "abc12345", "Shop", "server", nil)
	mux := newMux(newTestAPI(store))

	w, _ := doEndpoint(t, mux, "PATCH", "/api/endpoints/ep-1", `{"slug_name":"-bad-","name":"changed"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if ep, _ := store.GetEndpointByID(t.Context(), "ep-1"); ep.Slug != "abc12345" || ep.Name != "Shop" {
		t.Errorf("invalid rename changed the endpoint: %+v", ep)
	}

	store.slugTakenTimes = 100
	w, _ = doEndpoint(t, mux, "PATCH", "/api/endpoints/ep-1", `{"slug_name":"ok"}`)
	if w.Code != http.StatusConflict {
		t.Errorf("all taken: status = %d, want 409", w.Code)
	}
}
