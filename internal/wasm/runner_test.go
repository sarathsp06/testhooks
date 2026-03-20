package wasm

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// newTestRunner creates a Runner with a small pool for testing.
func newTestRunner(t *testing.T) *Runner {
	t.Helper()
	log := zerolog.New(os.Stderr).Level(zerolog.Disabled)
	cfg := Config{
		Timeout:      5 * time.Second,
		PoolSize:     2,
		MemoryLimit:  32 * 1024 * 1024, // 32 MB
		MaxStackSize: 512 * 1024,       // 512 KB
	}
	r, err := New(context.Background(), cfg, log)
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}
	t.Cleanup(func() { r.Close(context.Background()) })
	return r
}

func TestTransform_Identity(t *testing.T) {
	r := newTestRunner(t)

	input := TransformInput{
		Method:      "POST",
		Path:        "/webhook",
		Headers:     map[string][]string{"Content-Type": {"application/json"}},
		Body:        `{"foo":"bar"}`,
		ContentType: "application/json",
	}

	// Script that returns the input unchanged.
	script := `function transform(req) { return req; }`

	out, err := r.Transform(context.Background(), script, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Body != input.Body {
		t.Errorf("body = %q, want %q", out.Body, input.Body)
	}
	if out.Path != input.Path {
		t.Errorf("path = %q, want %q", out.Path, input.Path)
	}
}

func TestTransform_ModifiesBody(t *testing.T) {
	r := newTestRunner(t)

	input := TransformInput{
		Method:      "POST",
		Path:        "/hook",
		Headers:     map[string][]string{},
		Body:        `{"count":1}`,
		ContentType: "application/json",
	}

	script := `function transform(req) {
		var data = JSON.parse(req.body);
		data.count = data.count + 1;
		data.added = true;
		req.body = JSON.stringify(data);
		return req;
	}`

	out, err := r.Transform(context.Background(), script, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify the body was modified
	if out.Body != `{"count":2,"added":true}` {
		t.Errorf("body = %q, want modified JSON", out.Body)
	}
}

func TestTransform_SetsDropFlag(t *testing.T) {
	r := newTestRunner(t)

	input := TransformInput{
		Method:      "GET",
		Path:        "/ping",
		Headers:     map[string][]string{},
		ContentType: "text/plain",
	}

	script := `function transform(req) {
		return { drop: true };
	}`

	out, err := r.Transform(context.Background(), script, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Drop {
		t.Error("expected drop=true")
	}
}

func TestTransform_NoTransformFunction(t *testing.T) {
	r := newTestRunner(t)

	input := TransformInput{
		Method:      "POST",
		Path:        "/test",
		Headers:     map[string][]string{},
		Body:        `hello`,
		ContentType: "text/plain",
	}

	// Script without a transform function — should pass through.
	script := `var x = 42;`

	out, err := r.Transform(context.Background(), script, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Body != input.Body {
		t.Errorf("body = %q, want %q (passthrough)", out.Body, input.Body)
	}
}

func TestTransform_ScriptError(t *testing.T) {
	r := newTestRunner(t)

	input := TransformInput{Method: "GET", Path: "/", Headers: map[string][]string{}}

	script := `function transform(req) { throw new Error("boom"); }`

	_, err := r.Transform(context.Background(), script, input)
	if err == nil {
		t.Fatal("expected error from throwing script")
	}
}

func TestTransform_ReturnsNull(t *testing.T) {
	r := newTestRunner(t)

	input := TransformInput{Method: "GET", Path: "/", Headers: map[string][]string{}}

	// transform returns null → JSON.stringify(null) = "null" string →
	// unmarshals into zero-value TransformOutput (no error).
	script := `function transform(req) { return null; }`

	out, err := r.Transform(context.Background(), script, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// All fields should be zero/empty since "null" unmarshals to empty struct.
	if out.Body != "" || out.Method != "" || out.Drop {
		t.Errorf("expected zero-value output, got %+v", out)
	}
}

func TestTransform_NotReady(t *testing.T) {
	r := newTestRunner(t)
	r.Close(context.Background())

	input := TransformInput{Method: "GET", Path: "/", Headers: map[string][]string{}}
	_, err := r.Transform(context.Background(), `function transform(req) { return req; }`, input)
	if err == nil {
		t.Fatal("expected error when runner is not ready")
	}
}

func TestRunResponseHandler_BasicHandler(t *testing.T) {
	r := newTestRunner(t)

	input := TransformInput{
		Method:      "POST",
		Path:        "/webhook",
		Headers:     map[string][]string{"Content-Type": {"application/json"}},
		Body:        `{"event":"test"}`,
		ContentType: "application/json",
	}

	script := `function handler(req) {
		return {
			status: 201,
			headers: { "X-Custom": "yes" },
			body: "accepted: " + req.method,
			content_type: "text/plain"
		};
	}`

	out, err := r.RunResponseHandler(context.Background(), script, input, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == nil {
		t.Fatal("expected non-nil output")
	}
	if out.Status != 201 {
		t.Errorf("status = %d, want 201", out.Status)
	}
	if out.Headers["X-Custom"] != "yes" {
		t.Errorf("header X-Custom = %q, want %q", out.Headers["X-Custom"], "yes")
	}
	if out.Body != "accepted: POST" {
		t.Errorf("body = %q, want %q", out.Body, "accepted: POST")
	}
	if out.ContentType != "text/plain" {
		t.Errorf("content_type = %q, want %q", out.ContentType, "text/plain")
	}
}

func TestRunResponseHandler_NoHandler(t *testing.T) {
	r := newTestRunner(t)

	input := TransformInput{Method: "GET", Path: "/", Headers: map[string][]string{}}

	// No handler function — should return nil, nil.
	script := `var x = 1;`

	out, err := r.RunResponseHandler(context.Background(), script, input, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != nil {
		t.Errorf("expected nil output when no handler defined, got %+v", out)
	}
}

func TestRunResponseHandler_ReturnsNull(t *testing.T) {
	r := newTestRunner(t)

	input := TransformInput{Method: "GET", Path: "/", Headers: map[string][]string{}}

	script := `function handler(req) { return null; }`

	out, err := r.RunResponseHandler(context.Background(), script, input, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != nil {
		t.Errorf("expected nil output, got %+v", out)
	}
}

func TestRunResponseHandler_ScriptError(t *testing.T) {
	r := newTestRunner(t)

	input := TransformInput{Method: "GET", Path: "/", Headers: map[string][]string{}}

	script := `function handler(req) { throw new Error("fail"); }`

	_, err := r.RunResponseHandler(context.Background(), script, input, nil)
	if err == nil {
		t.Fatal("expected error from throwing handler")
	}
}

func TestRunResponseHandler_NotReady(t *testing.T) {
	r := newTestRunner(t)
	r.Close(context.Background())

	input := TransformInput{Method: "GET", Path: "/", Headers: map[string][]string{}}
	_, err := r.RunResponseHandler(context.Background(), `function handler(req) { return {status:200}; }`, input, nil)
	if err == nil {
		t.Fatal("expected error when runner is not ready")
	}
}

func TestRunResponseHandler_WithForwardResponse(t *testing.T) {
	r := newTestRunner(t)

	input := TransformInput{
		Method:      "POST",
		Path:        "/webhook",
		Headers:     map[string][]string{"Content-Type": {"application/json"}},
		Body:        `{"event":"payment"}`,
		ContentType: "application/json",
	}

	fwdResp := &ForwardResponse{
		Status:      200,
		Headers:     map[string]string{"X-Api-Id": "abc123"},
		Body:        `{"processed":true,"id":"txn_456"}`,
		ContentType: "application/json",
	}

	script := `function handler(req) {
		var fwd = req.forward_response;
		if (!fwd) return { status: 500, body: "no forward_response" };
		var fwdData = JSON.parse(fwd.body);
		return {
			status: fwd.status,
			body: JSON.stringify({ event: JSON.parse(req.body).event, txn: fwdData.id }),
			content_type: "application/json"
		};
	}`

	out, err := r.RunResponseHandler(context.Background(), script, input, fwdResp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == nil {
		t.Fatal("expected non-nil output")
	}
	if out.Status != 200 {
		t.Errorf("status = %d, want 200", out.Status)
	}
	if out.Body != `{"event":"payment","txn":"txn_456"}` {
		t.Errorf("body = %q, want JSON with event and txn", out.Body)
	}
}

func TestRunResponseHandler_ForwardResponseNilOmitted(t *testing.T) {
	r := newTestRunner(t)

	input := TransformInput{
		Method:  "GET",
		Path:    "/test",
		Headers: map[string][]string{},
	}

	// Handler checks that forward_response is undefined/null when not provided.
	script := `function handler(req) {
		var hasFwd = req.forward_response !== undefined && req.forward_response !== null;
		return { status: hasFwd ? 500 : 200, body: hasFwd ? "unexpected" : "ok" };
	}`

	out, err := r.RunResponseHandler(context.Background(), script, input, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == nil {
		t.Fatal("expected non-nil output")
	}
	if out.Status != 200 {
		t.Errorf("status = %d, want 200 (forward_response should be omitted)", out.Status)
	}
}

func TestTransform_WithQueryParams(t *testing.T) {
	r := newTestRunner(t)

	input := TransformInput{
		Method:      "GET",
		Path:        "/search",
		Headers:     map[string][]string{},
		Query:       map[string][]string{"q": {"hello"}, "page": {"1"}},
		ContentType: "",
	}

	script := `function transform(req) {
		req.body = "query=" + req.query.q[0];
		return req;
	}`

	out, err := r.Transform(context.Background(), script, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Body != "query=hello" {
		t.Errorf("body = %q, want %q", out.Body, "query=hello")
	}
}

func TestRunResponseHandler_DynamicFromRequest(t *testing.T) {
	r := newTestRunner(t)

	input := TransformInput{
		Method:      "POST",
		Path:        "/api/data",
		Headers:     map[string][]string{"Authorization": {"Bearer token123"}},
		Body:        `{"id":42}`,
		ContentType: "application/json",
	}

	script := `function handler(req) {
		var data = JSON.parse(req.body);
		return {
			status: 200,
			body: JSON.stringify({ received_id: data.id, path: req.path }),
			content_type: "application/json"
		};
	}`

	out, err := r.RunResponseHandler(context.Background(), script, input, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status != 200 {
		t.Errorf("status = %d, want 200", out.Status)
	}
	// Verify the body contains expected data
	if out.Body != `{"received_id":42,"path":"/api/data"}` {
		t.Errorf("body = %q, want JSON with received_id and path", out.Body)
	}
}

func TestReady(t *testing.T) {
	r := newTestRunner(t)
	if !r.Ready() {
		t.Error("expected Ready() = true after init")
	}
	r.Close(context.Background())
	if r.Ready() {
		t.Error("expected Ready() = false after Close()")
	}
}

func TestCloseIdempotent(t *testing.T) {
	r := newTestRunner(t)
	if err := r.Close(context.Background()); err != nil {
		t.Errorf("first close: %v", err)
	}
	if err := r.Close(context.Background()); err != nil {
		t.Errorf("second close: %v", err)
	}
}

func TestQuote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`hello`, `"hello"`},
		{`he said "hi"`, `"he said \"hi\""`},
		{"line1\nline2", `"line1\nline2"`},
		{`back\slash`, `"back\\slash"`},
	}
	for _, tt := range tests {
		got := quote(tt.input)
		if got != tt.want {
			t.Errorf("quote(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
