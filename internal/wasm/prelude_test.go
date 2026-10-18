package wasm

import (
	"context"
	"strings"
	"testing"
)

// Covers the script API from prelude.js: req.json, header helpers, null to skip.
func TestTransform_Prelude(t *testing.T) {
	r := newTestRunner(t)
	jsonReq := TransformInput{
		Method:      "POST",
		Path:        "/events",
		Headers:     map[string][]string{"Content-Type": {"application/json"}, "X-Event": {"invoice.paid"}},
		Body:        "{ \"id\": 1,\n  \"internal\": \"x\" }",
		ContentType: "application/json",
	}
	textReq := TransformInput{Method: "POST", Path: "/", Headers: map[string][]string{}, Body: "a=1", ContentType: "text/plain"}

	tests := []struct {
		name    string
		input   TransformInput
		script  string
		check   func(t *testing.T, out *TransformOutput)
		wantErr string
	}{
		{
			name:   "json edits are written back to the body",
			input:  jsonReq,
			script: `function transform(req) { req.json.ok = true; delete req.json.internal; return req; }`,
			check: func(t *testing.T, out *TransformOutput) {
				if out.Body != `{"id":1,"ok":true}` {
					t.Errorf("body = %q", out.Body)
				}
			},
		},
		{
			name:   "untouched json keeps the original bytes",
			input:  jsonReq,
			script: `function transform(req) { req.path = "/x"; return req; }`,
			check: func(t *testing.T, out *TransformOutput) {
				if out.Body != jsonReq.Body || out.Path != "/x" {
					t.Errorf("got body %q path %q", out.Body, out.Path)
				}
			},
		},
		{
			name:   "json is null for a non-JSON body",
			input:  textReq,
			script: `function transform(req) { req.body = String(req.json); return req; }`,
			check: func(t *testing.T, out *TransformOutput) {
				if out.Body != "null" {
					t.Errorf("body = %q", out.Body)
				}
			},
		},
		{
			name:   "replacing req.json sets a JSON content type",
			input:  textReq,
			script: `function transform(req) { req.json = { wrapped: req.body }; req.content_type = ""; return req; }`,
			check: func(t *testing.T, out *TransformOutput) {
				if out.Body != `{"wrapped":"a=1"}` || out.ContentType != "application/json" {
					t.Errorf("got body %q type %q", out.Body, out.ContentType)
				}
			},
		},
		{
			name:  "header helpers are case-insensitive",
			input: jsonReq,
			script: `function transform(req) {
				req.setHeader("x-event-type", req.header("x-EVENT"));
				req.removeHeader("x-event");
				req.setHeader("content-type", "application/json; charset=utf-8");
				return req;
			}`,
			check: func(t *testing.T, out *TransformOutput) {
				if got := out.Headers["x-event-type"]; len(got) != 1 || got[0] != "invoice.paid" {
					t.Errorf("x-event-type = %v", got)
				}
				if _, ok := out.Headers["X-Event"]; ok {
					t.Error("X-Event not removed")
				}
				if _, ok := out.Headers["Content-Type"]; ok {
					t.Error("old Content-Type key kept alongside the new one")
				}
			},
		},
		{
			name:   "returning a new object works",
			input:  jsonReq,
			script: `function transform(req) { return { method: "PUT", body: "hi" }; }`,
			check: func(t *testing.T, out *TransformOutput) {
				if out.Method != "PUT" || out.Body != "hi" {
					t.Errorf("got %+v", out)
				}
			},
		},
		{
			name:   "null skips the request",
			input:  jsonReq,
			script: `function transform(req) { return req.header("x-event") === "invoice.paid" ? null : req; }`,
			check: func(t *testing.T, out *TransformOutput) {
				if !out.Drop {
					t.Errorf("expected Drop, got %+v", out)
				}
			},
		},
		{
			name:    "forgetting to return is an error",
			input:   jsonReq,
			script:  `function transform(req) { req.json.a = 1; }`,
			wantErr: "returned nothing",
		},
		{
			name:    "returning a string is an error",
			input:   jsonReq,
			script:  `function transform(req) { return "x"; }`,
			wantErr: "must return the request object",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := r.Transform(context.Background(), tt.script, tt.input)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tt.check(t, out)
		})
	}
}

func TestRunResponseHandler_Prelude(t *testing.T) {
	r := newTestRunner(t)
	input := TransformInput{
		Method:  "POST",
		Path:    "/",
		Headers: map[string][]string{"X-Request-Id": {"abc"}},
		Body:    `{"name":"Ada"}`,
	}
	script := `function handler(req) { return { status: 201, body: req.json.name + " " + req.header("x-request-id") }; }`
	out, err := r.RunResponseHandler(context.Background(), script, input, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status != 201 || out.Body != "Ada abc" {
		t.Errorf("got %+v", out)
	}
}
