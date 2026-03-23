// Package wasm provides server-side JavaScript transform execution using
// QuickJS compiled to WASM via github.com/fastschema/qjs (wazero-based).
//
// User-provided JS scripts are executed in a sandboxed WASM environment on
// every captured webhook request (server mode only). A pool of QuickJS
// runtimes is maintained for concurrent execution.
package wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/fastschema/qjs"
	"github.com/rs/zerolog"
)

// TransformInput is the JSON payload passed to the user's transform function.
type TransformInput struct {
	Method      string              `json:"method"`
	Path        string              `json:"path"`
	Headers     map[string][]string `json:"headers"`
	Query       map[string][]string `json:"query,omitempty"`
	Body        string              `json:"body,omitempty"`
	ContentType string              `json:"content_type"`
}

// TransformOutput is the expected JSON result from the user's transform function.
type TransformOutput struct {
	Method      string              `json:"method,omitempty"`
	Path        string              `json:"path,omitempty"`
	Headers     map[string][]string `json:"headers,omitempty"`
	Body        string              `json:"body,omitempty"`
	ContentType string              `json:"content_type,omitempty"`
	// Drop, if true, means this request should not be forwarded.
	Drop bool `json:"drop,omitempty"`
}

// ForwardResponse represents the response received from a sync forward target.
// Passed to the response handler script as req.forward_response when sync forwarding is configured.
type ForwardResponse struct {
	Status      int               `json:"status"`
	Headers     map[string]string `json:"headers,omitempty"`
	Body        string            `json:"body,omitempty"`
	ContentType string            `json:"content_type,omitempty"`
}

// ResponseHandlerOutput is the expected JSON result from the user's handler() function.
// The handler receives the request and returns what the capture endpoint should respond with.
type ResponseHandlerOutput struct {
	Status      int               `json:"status,omitempty"`       // HTTP status code (default 200)
	Headers     map[string]string `json:"headers,omitempty"`      // response headers
	Body        string            `json:"body,omitempty"`         // response body
	ContentType string            `json:"content_type,omitempty"` // Content-Type header shortcut
}

// Runner manages QuickJS WASM runtimes and executes JS transforms.
// Each execution creates a fresh runtime to prevent state leakage between
// user scripts (MED-005).
type Runner struct {
	option  qjs.Option
	log     zerolog.Logger
	timeout time.Duration
	ready   bool
}

// Config for the WASM runner.
type Config struct {
	// Timeout for each transform execution (milliseconds sent to QuickJS).
	Timeout time.Duration
	// PoolSize is the number of QuickJS runtimes kept in the pool.
	PoolSize int
	// MemoryLimit in bytes for each QuickJS runtime.
	MemoryLimit int
	// MaxStackSize in bytes for each QuickJS runtime.
	MaxStackSize int
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		Timeout:      5 * time.Second,
		PoolSize:     4,
		MemoryLimit:  64 * 1024 * 1024, // 64 MB
		MaxStackSize: 1024 * 1024,      // 1 MB
	}
}

// New creates and initializes the WASM runner. Each script execution will
// create a fresh QuickJS runtime to prevent cross-script state leakage.
func New(_ context.Context, cfg Config, log zerolog.Logger) (*Runner, error) {
	l := log.With().Str("component", "wasm").Logger()

	option := qjs.Option{
		MemoryLimit:      cfg.MemoryLimit,
		MaxStackSize:     cfg.MaxStackSize,
		MaxExecutionTime: int(cfg.Timeout.Milliseconds()),
	}

	r := &Runner{
		option:  option,
		log:     l,
		timeout: cfg.Timeout,
		ready:   true,
	}

	l.Info().
		Int("memory_limit_mb", cfg.MemoryLimit/(1024*1024)).
		Dur("timeout", cfg.Timeout).
		Msg("WASM runner initialized (fresh runtime per execution)")

	return r, nil
}

// Close shuts down the runner. No pool to drain since runtimes are created
// fresh per execution.
func (r *Runner) Close(_ context.Context) error {
	r.ready = false
	return nil
}

// Ready returns true if the WASM runner is initialized and ready to execute scripts.
func (r *Runner) Ready() bool {
	return r.ready
}

// Transform executes a user-provided JS script against the given input.
// The script must define a function `transform(req)` that returns a transformed object.
//
// Returns the transform output, or an error if the script fails.
func (r *Runner) Transform(ctx context.Context, script string, input TransformInput) (*TransformOutput, error) {
	if !r.ready {
		return nil, fmt.Errorf("wasm runner not ready")
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal input: %w", err)
	}

	// Create a fresh runtime for this execution (MED-005: no state leakage).
	rt, err := qjs.New(r.option)
	if err != nil {
		return nil, fmt.Errorf("create runtime: %w", err)
	}
	defer rt.Close()

	// Build the JS program:
	// 1. Define the user's transform function(s)
	// 2. Parse the input JSON
	// 3. Call transform() if it exists, otherwise pass through
	// 4. Return the JSON-stringified result (last expression value)
	program := fmt.Sprintf(`
%s;

var __input = JSON.parse(%s);
var __result = typeof transform === 'function' ? transform(__input) : __input;
JSON.stringify(__result);
`, script, quote(string(inputJSON)))

	val, err := rt.Eval("transform.js", qjs.Code(program))
	if err != nil {
		return nil, fmt.Errorf("exec transform: %w", err)
	}
	defer val.Free()

	if val.IsUndefined() || val.IsNull() {
		return nil, fmt.Errorf("transform returned null/undefined")
	}

	resultJSON := val.String()

	var result TransformOutput
	if err := json.Unmarshal([]byte(resultJSON), &result); err != nil {
		return nil, fmt.Errorf("unmarshal output: %w (raw: %s)", err, resultJSON)
	}

	_ = ctx // context honoured by QuickJS MaxExecutionTime; kept for interface compat

	return &result, nil
}

// HandlerInput extends TransformInput with an optional forward_response field.
// This is the JSON object passed to the user's handler(req) function.
type HandlerInput struct {
	Method          string              `json:"method"`
	Path            string              `json:"path"`
	Headers         map[string][]string `json:"headers"`
	Query           map[string][]string `json:"query,omitempty"`
	Body            string              `json:"body,omitempty"`
	ContentType     string              `json:"content_type"`
	ForwardResponse *ForwardResponse    `json:"forward_response,omitempty"`
}

// RunResponseHandler executes a user-provided JS script that defines a handler(req)
// function returning {status, headers, body, content_type}. This is used for custom
// response generation — the webhook sender receives whatever the handler returns.
//
// The input is the post-transform request. If sync forwarding was used, fwdResp
// contains the forward target's response and is attached as req.forward_response
// in the handler script. Pass nil if no forward response is available.
//
// If the script doesn't define handler(), returns nil (fall through to default response).
func (r *Runner) RunResponseHandler(ctx context.Context, script string, input TransformInput, fwdResp *ForwardResponse) (*ResponseHandlerOutput, error) {
	if !r.ready {
		return nil, fmt.Errorf("wasm runner not ready")
	}

	// Build the handler input with optional forward_response.
	handlerInput := HandlerInput{
		Method:          input.Method,
		Path:            input.Path,
		Headers:         input.Headers,
		Query:           input.Query,
		Body:            input.Body,
		ContentType:     input.ContentType,
		ForwardResponse: fwdResp,
	}

	inputJSON, err := json.Marshal(handlerInput)
	if err != nil {
		return nil, fmt.Errorf("marshal input: %w", err)
	}

	// Create a fresh runtime for this execution (MED-005: no state leakage).
	rt, err := qjs.New(r.option)
	if err != nil {
		return nil, fmt.Errorf("create runtime: %w", err)
	}
	defer rt.Close()

	// Build the JS program:
	// 1. Define the user's handler function(s)
	// 2. Parse the input JSON (includes forward_response if present)
	// 3. Call handler() if it exists, otherwise return null (no custom response)
	// 4. Return the JSON-stringified result
	program := fmt.Sprintf(`
%s;

var __input = JSON.parse(%s);
var __result = typeof handler === 'function' ? handler(__input) : null;
JSON.stringify(__result);
`, script, quote(string(inputJSON)))

	val, err := rt.Eval("handler.js", qjs.Code(program))
	if err != nil {
		return nil, fmt.Errorf("exec handler: %w", err)
	}
	defer val.Free()

	if val.IsUndefined() || val.IsNull() {
		// No handler defined or handler returned null — use default response
		return nil, nil
	}

	resultJSON := val.String()
	if resultJSON == "null" {
		return nil, nil
	}

	var result ResponseHandlerOutput
	if err := json.Unmarshal([]byte(resultJSON), &result); err != nil {
		return nil, fmt.Errorf("unmarshal handler output: %w (raw: %s)", err, resultJSON)
	}

	_ = ctx // context honoured by QuickJS MaxExecutionTime

	return &result, nil
}

// quote returns a JS string literal wrapping s, safe for embedding in JS source.
func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
