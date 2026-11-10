package handler

import (
	_ "embed"
	"net/http"
)

// openAPI describes the REST API. It is served as is, so clients, agents and
// code generators can read what this server offers without the docs site.
//
//go:embed openapi.json
var openAPI []byte

// OpenAPI handles GET /api/openapi.json. It holds no data, so it needs no token.
func OpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(openAPI)
}
