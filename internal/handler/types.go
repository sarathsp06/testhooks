package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/sarathsp06/testhooks/internal/db"
	"github.com/sarathsp06/testhooks/internal/typegen"
)

// typesResult is what the types routes answer with.
type typesResult struct {
	Lang string `json:"lang"`
	Name string `json:"name"`
	// From is how many bodies the type was inferred from.
	From int    `json:"from"`
	Code string `json:"code"`
}

// maxTypesBodies caps how many requests one type is inferred from.
const maxTypesBodies = 200

// TypesForBody handles POST /api/types: a type for one or more JSON bodies
// given inline, for a client that holds a body the server never stored, such
// as a browser-mode tab. Body: {"lang": "...", "name": "...", "body": "..."}
// or "bodies": ["...", ...]. It runs no user code and stores nothing.
func (a *API) TypesForBody(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Lang   string   `json:"lang"`
		Name   string   `json:"name"`
		Body   *string  `json:"body"`
		Bodies []string `json:"bodies"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20)).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	var bodies [][]byte
	if in.Body != nil {
		bodies = append(bodies, []byte(*in.Body))
	}
	for _, b := range in.Bodies {
		bodies = append(bodies, []byte(b))
	}
	if len(bodies) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "give body (one JSON body as a string) or bodies (several)"})
		return
	}
	if len(bodies) > maxTypesBodies {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "at most 200 bodies"})
		return
	}
	a.writeTypes(w, in.Lang, in.Name, bodies)
}

// TypesForRequest handles GET /api/requests/{reqId}/types?lang=: a type for
// a captured request's body.
func (a *API) TypesForRequest(w http.ResponseWriter, r *http.Request) {
	req, err := a.db.GetRequest(r.Context(), r.PathValue("reqId"))
	if err != nil {
		a.writeLookupError(w, err, "request")
		return
	}
	q := r.URL.Query()
	a.writeTypes(w, q.Get("lang"), q.Get("name"), [][]byte{req.Body})
}

// TypesForEndpoint handles GET /api/endpoints/{id}/types?lang=&last=20: one
// type inferred from the endpoint's last N requests merged, so a field that
// only some of them carry comes out optional. Requests whose body isn't JSON,
// or that carry a different event than the newest one, are skipped; the type
// is named after that event.
func (a *API) TypesForEndpoint(w http.ResponseWriter, r *http.Request) {
	ep, err := a.db.GetEndpointByID(r.Context(), r.PathValue("id"))
	if err != nil {
		a.writeLookupError(w, err, "endpoint")
		return
	}
	q := r.URL.Query()
	last := 20
	if v := q.Get("last"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxTypesBodies {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "last must be a number of requests, 1 to 200"})
			return
		}
		last = n
	}
	reqs, err := a.db.ListRequests(r.Context(), ep.ID, last, 0)
	if err != nil {
		a.log.Error().Err(err).Msg("types: list requests")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	// One type is one event: with the stores listing newest first, merge
	// only requests that carry the newest JSON request's event, so a
	// customer.created never leaks into InvoicePaidPayload. A newest
	// request without an event field merges with every JSON body.
	var bodies [][]byte
	event, eventKnown := "", false
	for _, req := range reqs {
		if !json.Valid(req.Body) {
			continue
		}
		if !eventKnown {
			event, eventKnown = typegen.EventOf(req.Body), true
		}
		if event == "" || typegen.EventOf(req.Body) == event {
			bodies = append(bodies, req.Body)
		}
	}
	if len(bodies) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "the endpoint has no requests with a JSON body yet; send one with POST to its capture URL"})
		return
	}
	a.writeTypes(w, q.Get("lang"), q.Get("name"), bodies)
}

// writeTypes generates the type and answers. An empty lang means TypeScript;
// an empty name is taken from the first body's event field.
func (a *API) writeTypes(w http.ResponseWriter, langName, name string, bodies [][]byte) {
	if langName == "" {
		langName = string(typegen.TypeScript)
	}
	lang, err := typegen.ParseLang(langName)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if name == "" {
		name = typegen.TypeName(typegen.EventOf(bodies[0]))
	} else if !typegen.ValidName(name) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name must be a type name: letters, digits and underscores, not starting with a digit, e.g. InvoicePaidPayload"})
		return
	}
	code, err := typegen.Generate(lang, name, bodies...)
	if err != nil {
		status := http.StatusUnprocessableEntity
		if errors.Is(err, db.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, typesResult{Lang: string(lang), Name: name, From: len(bodies), Code: code})
}
