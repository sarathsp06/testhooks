package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sarathsp06/testhooks/internal/db"
	"github.com/sarathsp06/testhooks/internal/wasm"
)

func newPreviewAPI(t *testing.T, cfg string) (*http.ServeMux, *mockStore) {
	t.Helper()
	store := newMockStore()
	store.seedEndpoint("ep-1", "abc", "test", "server", json.RawMessage(cfg))
	store.seedEndpoint("ep-2", "def", "other", "server", json.RawMessage(`{}`))
	_ = store.InsertRequest(t.Context(), &db.CapturedRequest{
		EndpointID: "ep-1", Method: "POST", Path: "/events",
		Headers:     json.RawMessage(`{"Content-Type":["application/json"],"X-Secret-Host":["platform"]}`),
		Query:       json.RawMessage(`{"debug":["1"]}`),
		Body:        []byte(`{"event":"order.created","internal_id":7}`),
		ContentType: "application/json",
	})
	api := newTestAPI(store)
	api.SetHideHeaders([]string{"X-Secret-Host"})
	runner, err := wasm.New(context.Background(), wasm.Config{Timeout: 5 * time.Second, PoolSize: 1, MemoryLimit: 32 << 20, MaxStackSize: 512 << 10}, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runner.Close(context.Background()) })
	api.SetScriptRunner(runner)
	return newMux(api), store
}

func preview(t *testing.T, mux *http.ServeMux, id, body string) (int, previewResult, string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/endpoints/"+id+"/preview", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(w, req)
	var out previewResult
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out, w.Body.String()
}

func TestPreview_TransformAndReplyOnCapturedRequest(t *testing.T) {
	mux, _ := newPreviewAPI(t, `{}`)
	code, out, body := preview(t, mux, "ep-1", `{
		"request_id": "req-1",
		"transform": "function transform(req) { delete req.json.internal_id; req.json.seen = true; req.setHeader('x-source', 'preview'); return req; }",
		"reply": "function handler(req) { return { status: 201, body: 'got ' + req.json.event, headers: { 'x-ok': '1' } }; }"
	}`)
	if code != 200 {
		t.Fatalf("got %d %s", code, body)
	}
	if out.Input.Method != "POST" || out.Input.Path != "/events" || out.Input.Query["debug"][0] != "1" {
		t.Fatalf("input = %+v", out.Input)
	}
	if _, hidden := out.Input.Headers["X-Secret-Host"]; hidden {
		t.Fatal("hidden header reached the script input")
	}
	if out.Transform == nil || out.Transform.Forwarded == nil || out.Transform.Error != "" {
		t.Fatalf("transform = %+v", out.Transform)
	}
	fwd := out.Transform.Forwarded
	if !strings.Contains(fwd.Body, `"seen":true`) || strings.Contains(fwd.Body, "internal_id") || fwd.Headers["x-source"][0] != "preview" {
		t.Fatalf("forwarded = %+v", fwd)
	}
	if out.Reply.Status != 201 || out.Reply.Body != "got order.created" || out.Reply.Source != "handler" || out.Reply.Headers["x-ok"] != "1" || out.Reply.ContentType != "text/plain" {
		t.Fatalf("reply = %+v", out.Reply)
	}
}

func TestPreview_UsesSavedScriptsAndInlineRequest(t *testing.T) {
	mux, _ := newPreviewAPI(t, `{
		"wasm_script": "function transform(req) { req.body = 'saved'; return req; }",
		"custom_response": {"enabled": true, "script": "function handler(req) { return { status: 204 }; }"}
	}`)
	code, out, body := preview(t, mux, "ep-1", `{"request": {"method": "put", "body": "{\"a\":1}", "headers": {"Content-Type": ["application/json"]}}}`)
	if code != 200 {
		t.Fatalf("got %d %s", code, body)
	}
	if out.Input.Method != "put" || out.Input.Path != "/" || out.Input.ContentType != "application/json" {
		t.Fatalf("input = %+v", out.Input)
	}
	if out.Transform == nil || out.Transform.Forwarded == nil || out.Transform.Forwarded.Body != "saved" {
		t.Fatalf("transform = %+v", out.Transform)
	}
	if out.Reply.Status != 204 || out.Reply.Source != "handler" {
		t.Fatalf("reply = %+v", out.Reply)
	}

	// An empty string turns a saved script off for this preview.
	_, out, _ = preview(t, mux, "ep-1", `{"request_id": "req-1", "transform": "", "reply": ""}`)
	if out.Transform != nil || out.Reply.Source != "default" || out.Reply.Status != 200 || !strings.Contains(out.Reply.Body, `"id":"req-1"`) {
		t.Fatalf("got transform %+v reply %+v", out.Transform, out.Reply)
	}
}

func TestPreview_DropAndErrorsAreReported(t *testing.T) {
	mux, _ := newPreviewAPI(t, `{}`)
	_, out, _ := preview(t, mux, "ep-1", `{"request_id": "req-1", "transform": "function transform(req) { return null; }", "reply": "function handler(req) { return {status: 500}; }"}`)
	if out.Transform == nil || !out.Transform.Dropped || out.Transform.Forwarded != nil {
		t.Fatalf("transform = %+v", out.Transform)
	}
	if out.Reply.Source != "default" || out.Reply.Status != 200 {
		t.Fatalf("dropped request should get the default reply, got %+v", out.Reply)
	}

	_, out, _ = preview(t, mux, "ep-1", `{"request_id": "req-1", "transform": "function transform(req) { throw new Error('boom'); }", "reply": "function handler(req) { nope(); }"}`)
	if out.Transform == nil || !strings.Contains(out.Transform.Error, "boom") || out.Transform.Forwarded != nil {
		t.Fatalf("transform = %+v", out.Transform)
	}
	if out.Reply.Source != "default" {
		t.Fatalf("failed transform should stop at the default reply, got %+v", out.Reply)
	}

	_, out, _ = preview(t, mux, "ep-1", `{"request_id": "req-1", "reply": "function handler(req) { nope(); }"}`)
	if out.Reply.Source != "default" || !strings.Contains(out.Reply.Error, "nope") {
		t.Fatalf("reply = %+v", out.Reply)
	}
}

func TestPreview_ForwardResponsePassthrough(t *testing.T) {
	mux, _ := newPreviewAPI(t, `{}`)
	_, out, _ := preview(t, mux, "ep-1", `{"request_id": "req-1", "forward_response": {"status": 418, "body": "teapot"}}`)
	if out.Reply.Source != "forward_passthrough" || out.Reply.Status != 418 || out.Reply.Body != "teapot" {
		t.Fatalf("reply = %+v", out.Reply)
	}
	_, out, _ = preview(t, mux, "ep-1", `{"request_id": "req-1", "forward_response": {"status": 418, "body": "teapot"}, "reply": "function handler(req) { return { status: req.forward_response.status + 1 }; }"}`)
	if out.Reply.Source != "handler" || out.Reply.Status != 419 {
		t.Fatalf("reply = %+v", out.Reply)
	}
}

func TestPreview_Errors(t *testing.T) {
	mux, _ := newPreviewAPI(t, `{}`)
	for _, tc := range []struct {
		id, body string
		want     int
		msg      string
	}{
		{"ep-1", `{}`, 400, "request_id"},
		{"ep-1", `{"request_id": "req-1", "request": {}}`, 400, "not both"},
		{"ep-1", `{"request_id": "nope"}`, 404, "request not found"},
		{"ep-2", `{"request_id": "req-1"}`, 400, "this endpoint"},
		{"nope", `{"request_id": "req-1"}`, 404, "endpoint not found"},
		{"ep-1", `not json`, 400, "invalid JSON"},
	} {
		code, _, body := preview(t, mux, tc.id, tc.body)
		if code != tc.want || !strings.Contains(body, tc.msg) {
			t.Errorf("%s %s: got %d %s, want %d %q", tc.id, tc.body, code, body, tc.want, tc.msg)
		}
	}

	store := newMockStore()
	store.seedEndpoint("ep-1", "abc", "test", "server", json.RawMessage(`{}`))
	api := newTestAPI(store)
	mux = http.NewServeMux()
	mux.HandleFunc("POST /api/endpoints/{id}/preview", api.PreviewScript)
	code, _, body := preview(t, mux, "ep-1", `{"request_id": "req-1"}`)
	if code != 503 || !strings.Contains(body, "can't run") {
		t.Fatalf("no runner: got %d %s", code, body)
	}
}
