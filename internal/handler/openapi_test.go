package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestOpenAPI_DescribesEveryRoute keeps the document honest: each API route
// main.go serves must appear in it, and it must be valid JSON.
func TestOpenAPI_DescribesEveryRoute(t *testing.T) {
	w := httptest.NewRecorder()
	OpenAPI(w, httptest.NewRequest("GET", "/api/openapi.json", nil))
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("got %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	var doc struct {
		OpenAPI string                            `json:"openapi"`
		Paths   map[string]map[string]interface{} `json:"paths"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if doc.OpenAPI != "3.1.0" {
		t.Fatalf("openapi = %q", doc.OpenAPI)
	}
	want := map[string][]string{
		"/api/endpoints":               {"post", "get"},
		"/api/endpoints/{id}":          {"get", "patch", "delete"},
		"/api/slug/{slug}":             {"get"},
		"/api/endpoints/{id}/requests": {"get", "delete"},
		"/api/requests/{reqId}":        {"get", "delete"},
		"/api/endpoints/{id}/preview":  {"post"},
		"/api/endpoints/{id}/resign":   {"post"},
		"/api/limits":                  {"get"},
		"/api/upgrade-interest":        {"post"},
		"/api/openapi.json":            {"get"},
		"/healthz":                     {"get"},
		"/h/{slug}":                    {"post"},
		"/h/{slug}/{path}":             {"post"},
	}
	for path, methods := range want {
		item, ok := doc.Paths[path]
		if !ok {
			t.Errorf("%s is not described", path)
			continue
		}
		for _, m := range methods {
			if _, ok := item[m]; !ok {
				t.Errorf("%s %s is not described", m, path)
			}
		}
	}
}
