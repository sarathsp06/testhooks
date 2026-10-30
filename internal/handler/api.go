package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/rs/zerolog"
	"github.com/sarathsp06/testhooks/internal/config"
	"github.com/sarathsp06/testhooks/internal/crypto"
	"github.com/sarathsp06/testhooks/internal/db"
	"github.com/sarathsp06/testhooks/internal/slug"
)

// API handles REST endpoints for managing endpoints and viewing requests.
type API struct {
	db  Store
	hub interface {
		RemoveBuffer(slug string)
	}
	log     zerolog.Logger
	secrets secretBox
	limits  Limits
	// hideHeaders are left out of served requests unless ?include_hidden=true.
	hideHeaders headerMatcher
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

// SetEncryption sets the service that encrypts signature secrets at rest.
// Without one, saving a secret is rejected.
func (a *API) SetEncryption(svc *crypto.Service) {
	a.secrets = secretBox{svc: svc}
}

// SetHideHeaders sets the headers left out of served requests unless the
// caller asks for them with ?include_hidden=true. See config.Config.HideHeaders.
func (a *API) SetHideHeaders(patterns []string) {
	a.hideHeaders = newHeaderMatcher(patterns)
}

// shown returns req as served: without hidden headers unless r asks for them.
func (a *API) shown(r *http.Request, req *db.CapturedRequest) *db.CapturedRequest {
	if r.URL.Query().Get("include_hidden") == "true" {
		return req
	}
	return a.hideHeaders.shown(req)
}

// sealConfig encrypts the signature secret in cfg, keeping the one in old when
// cfg doesn't set it. It writes the error response and returns false on failure.
func (a *API) sealConfig(w http.ResponseWriter, cfg, old json.RawMessage) (json.RawMessage, bool) {
	sealed, err := a.secrets.seal(cfg, old)
	if errors.Is(err, errNoEncryptionKeys) || errors.Is(err, errResignNeedsSecret) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return nil, false
	}
	if err != nil {
		a.log.Error().Err(err).Msg("failed to seal signature secret")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return nil, false
	}
	return sealed, true
}

// writeEndpoint responds with ep, its signature secret redacted.
func (a *API) writeEndpoint(w http.ResponseWriter, status int, ep *db.Endpoint) {
	writeJSON(w, status, a.secrets.redact(*ep))
}

// --- Endpoint handlers ---

// CreateEndpoint handles POST /api/endpoints.
func (a *API) CreateEndpoint(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string          `json:"name"`
		SlugName string          `json:"slug_name"`
		Mode     string          `json:"mode"`
		Config   json.RawMessage `json:"config,omitempty"`
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
	slugName, err := slug.CleanName(body.SlugName)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Normalise nil/empty config to NULL.
	var cfg json.RawMessage
	if len(body.Config) > 0 && string(body.Config) != "null" {
		cfg = body.Config
	}
	cfg, ok := a.sealConfig(w, cfg, nil)
	if !ok {
		return
	}

	// Extract client identity for UX convenience (not access control).
	clientID := r.Header.Get("X-Client-ID")

	ep, err := withNewSlug(slugName, func(s string) (*db.Endpoint, error) {
		return a.db.CreateEndpoint(r.Context(), s, body.Name, body.Mode, clientID, cfg)
	})
	if err != nil {
		a.writeSlugError(w, err, "failed to create endpoint")
		return
	}
	a.writeEndpoint(w, http.StatusCreated, ep)
}

// slugAttempts is how many fresh slugs withNewSlug tries. With 2.2 billion
// word combinations per name, needing a second try is already rare.
const slugAttempts = 5

// withNewSlug calls save with a new slug for name (already cleaned), and again
// with another one while save reports db.ErrSlugTaken.
func withNewSlug(name string, save func(slug string) (*db.Endpoint, error)) (*db.Endpoint, error) {
	var err error
	for range slugAttempts {
		var s string
		if s, err = slug.New(name); err != nil {
			return nil, err
		}
		var ep *db.Endpoint
		if ep, err = save(s); !errors.Is(err, db.ErrSlugTaken) {
			return ep, err
		}
	}
	return nil, err
}

// writeSlugError responds to a failed create or rename: 409 when every slug
// tried was taken, 500 otherwise.
func (a *API) writeSlugError(w http.ResponseWriter, err error, msg string) {
	if errors.Is(err, db.ErrSlugTaken) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Couldn't find a free URL for that name. Try again or pick another name."})
		return
	}
	a.log.Error().Err(err).Msg(msg)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
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
		"endpoints": a.secrets.redactAll(endpoints),
		"limit":     limit,
		"offset":    offset,
	})
}

// GetEndpoint handles GET /api/endpoints/{id}.
func (a *API) GetEndpoint(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ep, err := a.db.GetEndpointByID(r.Context(), id)
	if err != nil {
		a.writeLookupError(w, err, "endpoint")
		return
	}
	a.writeEndpoint(w, http.StatusOK, ep)
}

// GetEndpointBySlug handles GET /api/slug/{slug}.
func (a *API) GetEndpointBySlug(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	ep, err := a.db.GetEndpointBySlug(r.Context(), slug)
	if err != nil {
		a.writeLookupError(w, err, "endpoint")
		return
	}
	a.writeEndpoint(w, http.StatusOK, ep)
}

// ResignRequest handles POST /api/endpoints/{id}/resign. In browser mode the
// tab transforms and forwards, but the webhook secret never leaves the server,
// so the tab sends the request as captured and the body it is about to
// forward, and gets back the webhook-* headers to send with it. The captured
// request must verify with the endpoint's HMAC secret now, as in server mode,
// so this can't be used to sign a body nobody signed.
func (a *API) ResignRequest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Request struct {
			ID           string      `json:"id"`
			Headers      http.Header `json:"headers"`
			Body         string      `json:"body"`
			BodyEncoding string      `json:"body_encoding"`
		} `json:"request"`
		Body string `json:"body"`
	}
	// Two bodies, one possibly base64; leave room for headers.
	r.Body = http.MaxBytesReader(w, r.Body, 3*a.limits.MaxBodySize+1<<20)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	original := []byte(in.Request.Body)
	if in.Request.BodyEncoding == "base64" {
		var err error
		if original, err = base64.StdEncoding.DecodeString(in.Request.Body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request.body is not valid base64"})
			return
		}
	}

	ep, err := a.db.GetEndpointByID(r.Context(), r.PathValue("id"))
	if err != nil {
		a.writeLookupError(w, err, "endpoint")
		return
	}
	cfg := config.ParseEndpointConfig(ep.Config)
	if cfg.Signature == nil || !cfg.Signature.Resign {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "re-signing is off for this endpoint (signature.resign)"})
		return
	}

	// Canonical names, whatever case the caller used.
	orig := http.Header{}
	for k, v := range in.Request.Headers {
		for _, s := range v {
			orig.Add(k, s)
		}
	}
	now := time.Now()
	incoming := a.secrets.check(cfg, orig, original, now)
	out := http.Header{}
	if id := orig.Get("webhook-id"); id != "" {
		out.Set("webhook-id", id)
	}
	signed := a.secrets.resign(cfg, incoming, out, "msg_"+in.Request.ID, []byte(in.Body), now)
	if !signed.Resigned {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": signed.Error})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"headers": map[string]string{
		"webhook-id":        out.Get("webhook-id"),
		"webhook-timestamp": out.Get("webhook-timestamp"),
		"webhook-signature": out.Get("webhook-signature"),
	}})
}

// UpdateEndpoint handles PATCH /api/endpoints/{id}.
func (a *API) UpdateEndpoint(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Get current endpoint first.
	existing, err := a.db.GetEndpointByID(r.Context(), id)
	if err != nil {
		a.writeLookupError(w, err, "endpoint")
		return
	}

	var body struct {
		Name     *string          `json:"name"`
		SlugName *string          `json:"slug_name"`
		Mode     *string          `json:"mode"`
		Config   *json.RawMessage `json:"config"`
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
		sealed, ok := a.sealConfig(w, *body.Config, existing.Config)
		if !ok {
			return
		}
		config = sealed
	}

	// Renaming gives the endpoint a new slug: the name, if any, and three
	// new words. The old URL stops working at once.
	if body.SlugName != nil {
		slugName, err := slug.CleanName(*body.SlugName)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		oldSlug := existing.Slug
		if _, err := withNewSlug(slugName, func(s string) (*db.Endpoint, error) {
			return a.db.RenameEndpoint(r.Context(), id, s)
		}); err != nil {
			a.writeSlugError(w, err, "failed to rename endpoint")
			return
		}
		if a.hub != nil {
			a.hub.RemoveBuffer(oldSlug)
		}
	}

	ep, err := a.db.UpdateEndpoint(r.Context(), id, name, mode, config)
	if err != nil {
		a.log.Error().Err(err).Msg("failed to update endpoint")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	a.writeEndpoint(w, http.StatusOK, ep)
}

// DeleteEndpoint handles DELETE /api/endpoints/{id}.
func (a *API) DeleteEndpoint(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Look up the endpoint first to get the slug for ring buffer cleanup.
	ep, err := a.db.GetEndpointByID(r.Context(), id)
	if err != nil {
		a.writeLookupError(w, err, "endpoint")
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
	ep, err := a.db.GetEndpointByID(r.Context(), endpointID)
	if err != nil {
		a.writeLookupError(w, err, "endpoint")
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
	epConfig := config.ParseEndpointConfig(ep.Config)
	for i := range reqs {
		a.secrets.annotate(epConfig, &reqs[i])
		reqs[i] = *a.shown(r, &reqs[i])
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
		a.writeLookupError(w, err, "request")
		return
	}
	if ep, err := a.db.GetEndpointByID(r.Context(), req.EndpointID); err == nil {
		a.secrets.annotate(config.ParseEndpointConfig(ep.Config), req)
	}
	writeJSON(w, http.StatusOK, a.shown(r, req))
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

// writeLookupError responds 404 when the record doesn't exist and 500 for any
// other failure, so a database outage isn't reported to clients as "not found".
func (a *API) writeLookupError(w http.ResponseWriter, err error, what string) {
	if errors.Is(err, db.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": what + " not found"})
		return
	}
	a.log.Error().Err(err).Str("lookup", what).Msg("lookup failed")
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
}
