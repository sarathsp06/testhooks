package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/rs/zerolog"
	"github.com/sarathsp06/testhooks/internal/db"
)

// API handles REST endpoints for managing endpoints and viewing requests.
type API struct {
	db  Store
	hub interface {
		RemoveBuffer(slug string)
	}
	log zerolog.Logger
}

// NewAPI creates a new API handler.
// The hub parameter is used to clean up ring buffers when endpoints are deleted.
func NewAPI(store Store, hub interface{ RemoveBuffer(slug string) }, log zerolog.Logger) *API {
	return &API{
		db:  store,
		hub: hub,
		log: log.With().Str("component", "api").Logger(),
	}
}

// --- Endpoint handlers ---

// CreateEndpoint handles POST /api/endpoints.
func (a *API) CreateEndpoint(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string          `json:"name"`
		Mode   string          `json:"mode"`
		Config json.RawMessage `json:"config,omitempty"`
	}
	// MED-002: Limit API request body size to prevent OOM from oversized payloads.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		// Defaults.
		body.Name = ""
		body.Mode = "server"
	}
	if body.Mode == "" {
		body.Mode = "server"
	}
	if body.Mode != "server" && body.Mode != "browser" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mode must be 'server' or 'browser'"})
		return
	}
	// L-08: Validate endpoint name length.
	if len(body.Name) > 255 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name must be at most 255 characters"})
		return
	}

	// Normalise nil/empty config to NULL.
	var cfg json.RawMessage
	if len(body.Config) > 0 && string(body.Config) != "null" {
		cfg = body.Config
	}

	// Extract client identity for UX convenience (not access control).
	clientID := r.Header.Get("X-Client-ID")

	slug := GenerateSlug()
	ep, err := a.db.CreateEndpoint(r.Context(), slug, body.Name, body.Mode, clientID, cfg)
	if err != nil {
		a.log.Error().Err(err).Msg("failed to create endpoint")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusCreated, ep)
}

// ListEndpoints handles GET /api/endpoints.
// H-03: Paginated to prevent unbounded result sets.
// Filters by X-Client-ID header when present for UX convenience.
func (a *API) ListEndpoints(w http.ResponseWriter, r *http.Request) {
	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	clientID := r.Header.Get("X-Client-ID")

	endpoints, err := a.db.ListEndpoints(r.Context(), clientID, limit, offset)
	if err != nil {
		a.log.Error().Err(err).Msg("failed to list endpoints")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if endpoints == nil {
		endpoints = []db.Endpoint{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"endpoints": endpoints,
		"limit":     limit,
		"offset":    offset,
	})
}

// GetEndpoint handles GET /api/endpoints/{id}.
func (a *API) GetEndpoint(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ep, err := a.db.GetEndpointByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "endpoint not found"})
		return
	}
	writeJSON(w, http.StatusOK, ep)
}

// GetEndpointBySlug handles GET /api/endpoints/by-slug/{slug}.
func (a *API) GetEndpointBySlug(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	ep, err := a.db.GetEndpointBySlug(r.Context(), slug)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "endpoint not found"})
		return
	}
	writeJSON(w, http.StatusOK, ep)
}

// UpdateEndpoint handles PATCH /api/endpoints/{id}.
func (a *API) UpdateEndpoint(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Get current endpoint first.
	existing, err := a.db.GetEndpointByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "endpoint not found"})
		return
	}

	var body struct {
		Name   *string          `json:"name"`
		Mode   *string          `json:"mode"`
		Config *json.RawMessage `json:"config"`
	}
	// MED-002: Limit API request body size to prevent OOM from oversized payloads.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}

	name := existing.Name
	mode := existing.Mode
	config := existing.Config

	if body.Name != nil {
		// L-08: Validate endpoint name length.
		if len(*body.Name) > 255 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name must be at most 255 characters"})
			return
		}
		name = *body.Name
	}
	if body.Mode != nil {
		if *body.Mode != "server" && *body.Mode != "browser" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mode must be 'server' or 'browser'"})
			return
		}
		mode = *body.Mode
	}
	if body.Config != nil {
		config = *body.Config
	}

	ep, err := a.db.UpdateEndpoint(r.Context(), id, name, mode, config)
	if err != nil {
		a.log.Error().Err(err).Msg("failed to update endpoint")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, ep)
}

// DeleteEndpoint handles DELETE /api/endpoints/{id}.
func (a *API) DeleteEndpoint(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Look up the endpoint first to get the slug for ring buffer cleanup.
	ep, err := a.db.GetEndpointByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "endpoint not found"})
		return
	}

	if err := a.db.DeleteEndpoint(r.Context(), id); err != nil {
		a.log.Error().Err(err).Msg("failed to delete endpoint")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	// LOW-003: Clean up ring buffer for the deleted endpoint.
	if a.hub != nil {
		a.hub.RemoveBuffer(ep.Slug)
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- Request handlers ---

// ListRequests handles GET /api/endpoints/{id}/requests.
func (a *API) ListRequests(w http.ResponseWriter, r *http.Request) {
	endpointID := r.PathValue("id")

	// Verify endpoint exists.
	if _, err := a.db.GetEndpointByID(r.Context(), endpointID); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "endpoint not found"})
		return
	}

	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	reqs, err := a.db.ListRequests(r.Context(), endpointID, limit, offset)
	if err != nil {
		a.log.Error().Err(err).Msg("failed to list requests")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if reqs == nil {
		reqs = []db.CapturedRequest{}
	}

	// Also return total count.
	total, _ := a.db.CountRequests(r.Context(), endpointID)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"requests": reqs,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
	})
}

// GetRequest handles GET /api/requests/{reqId}.
func (a *API) GetRequest(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("reqId")
	req, err := a.db.GetRequest(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "request not found"})
		return
	}
	writeJSON(w, http.StatusOK, req)
}

// DeleteRequest handles DELETE /api/requests/{reqId}.
func (a *API) DeleteRequest(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("reqId")
	if err := a.db.DeleteRequest(r.Context(), id); err != nil {
		a.log.Error().Err(err).Msg("failed to delete request")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteAllRequests handles DELETE /api/endpoints/{id}/requests.
func (a *API) DeleteAllRequests(w http.ResponseWriter, r *http.Request) {
	endpointID := r.PathValue("id")
	if err := a.db.DeleteAllRequests(r.Context(), endpointID); err != nil {
		a.log.Error().Err(err).Msg("failed to delete all requests")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
