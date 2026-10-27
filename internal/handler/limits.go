package handler

import (
	"crypto/rand"
	"encoding/json"
	"net/http"
)

// Limits are the storage limits the UI shows next to the request count, the
// optional note about a paid plan, and whether server-side forwarding may
// reach private addresses. They mirror the configuration.
type Limits struct {
	MaxRequestsPerEndpoint  int    `json:"max_requests_per_endpoint"`
	MaxEndpointStorageBytes int64  `json:"max_endpoint_storage_bytes"`
	MaxBodySize             int64  `json:"max_body_size"`
	UpgradeNote             string `json:"upgrade_note,omitempty"`
	ForwardPrivateAddresses bool   `json:"forward_private_addresses"`
}

// SetLimits sets what GET /api/limits returns.
func (a *API) SetLimits(l Limits) { a.limits = l }

// GetLimits handles GET /api/limits.
func (a *API) GetLimits(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.limits)
}

// RecordUpgradeInterest handles POST /api/upgrade-interest: the "I'd pay for
// that" click. One count per X-Client-ID; a caller without one counts once
// per call. Returns {"count": n}, the number of people who have asked.
func (a *API) RecordUpgradeInterest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		EndpointID string `json:"endpoint_id"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	_ = json.NewDecoder(r.Body).Decode(&body)
	if len(body.EndpointID) > 64 {
		body.EndpointID = body.EndpointID[:64]
	}
	clientID := r.Header.Get("X-Client-ID")
	if clientID == "" || len(clientID) > 128 {
		clientID = "anon-" + rand.Text()
	}
	count, err := a.db.RecordUpgradeInterest(r.Context(), clientID, body.EndpointID)
	if err != nil {
		a.log.Error().Err(err).Msg("failed to record upgrade interest")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	a.log.Info().Int("count", count).Str("endpoint_id", body.EndpointID).Msg("upgrade interest recorded")
	writeJSON(w, http.StatusOK, map[string]int{"count": count})
}
