// Package wasm provides server-side JavaScript transform execution using
// QuickJS compiled to WASM via github.com/fastschema/qjs (wazero-based).
//
// User-provided JS scripts are executed in a sandboxed WASM environment on
// every captured webhook request (server mode only). A pool of QuickJS
// runtimes is maintained for concurrent execution.
package wasm

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"time"

	"github.com/fastschema/qjs"
	"github.com/rs/zerolog"
)

// prelude is loaded ahead of every user script. The browser loads the same
// file (web/src/lib/wasm.ts), so scripts behave identically in both places.
//
//go:embed prelude.js
var prelude string

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
// Every execution starts from a freshly set-up runtime so nothing leaks
// between user scripts (MED-005): runtimes are reused, but their memory is
// reset to a snapshot taken right after setup (see sandbox).
type Runner struct {
	option        qjs.Option
	log           zerolog.Logger
	timeout       time.Duration
	ready         atomic.Bool // L-09: atomic for concurrent access
	maxOutputSize int         // M-07: max WASM output size in bytes
	// slots bounds concurrent runtimes so a burst of requests can't allocate
	// PoolSize×MemoryLimit without limit and OOM the process.
	slots chan struct{}
	// idle holds set-up runtimes waiting for their next script.
	idle chan *sandbox
	// sandboxDir is the empty directory scripts see as their filesystem.
	sandboxDir string
}

// Config for the WASM runner.
type Config struct {
	// Timeout for each transform execution (milliseconds sent to QuickJS).
	Timeout time.Duration
	// PoolSize is the maximum number of QuickJS runtimes executing at once.
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

	// qjs mounts Option.CWD into the sandbox read-write, and defaults it to the
	// process's working directory, so scripts could read and write the server's
	// files (.env, snapshots) through qjs's std and os modules. Give it an
	// empty, read-only directory instead. The prelude also removes std and os.
	sandboxDir, err := os.MkdirTemp("", "testhooks-js-")
	if err != nil {
		return nil, fmt.Errorf("create script sandbox dir: %w", err)
	}
	if err := os.Chmod(sandboxDir, 0o555); err != nil {
		return nil, fmt.Errorf("lock script sandbox dir: %w", err)
	}

	wasmBytes, err := cappedQJSWasm(cfg.MemoryLimit)
	if err != nil {
		return nil, fmt.Errorf("cap script memory: %w", err)
	}

	option := qjs.Option{
		CWD:              sandboxDir,
		QuickJSWasmBytes: wasmBytes,
		MemoryLimit:      cfg.MemoryLimit,
		MaxStackSize:     cfg.MaxStackSize,
		MaxExecutionTime: int(cfg.Timeout.Milliseconds()),
		Stdout:           io.Discard,
		Stderr:           io.Discard,
	}

	poolSize := cfg.PoolSize
	if poolSize <= 0 {
		poolSize = 1
	}

	r := &Runner{
		slots:         make(chan struct{}, poolSize),
		idle:          make(chan *sandbox, poolSize),
		option:        option,
		log:           l,
		timeout:       cfg.Timeout,
		maxOutputSize: 1024 * 1024, // M-07: 1MB default
		sandboxDir:    sandboxDir,
	}
	r.ready.Store(true)

	l.Info().
		Int("memory_limit_mb", cfg.MemoryLimit/(1024*1024)).
		Dur("timeout", cfg.Timeout).
		Int("max_concurrent", poolSize).
		Msg("WASM runner initialized (runtime reset after each execution)")

	return r, nil
}

// Close shuts down the runner and frees its idle runtimes.
func (r *Runner) Close(_ context.Context) error {
	// Not ready first, so a script finishing now closes its runtime instead
	// of returning it to idle.
	wasReady := r.ready.Swap(false)
drain:
	for {
		select {
		case sb := <-r.idle:
			sb.close()
		default:
			break drain
		}
	}
	if wasReady && r.sandboxDir != "" {
		_ = os.Chmod(r.sandboxDir, 0o700)
		_ = os.RemoveAll(r.sandboxDir)
	}
	return nil
}

// Ready returns true if the WASM runner is initialized and ready to execute scripts.
func (r *Runner) Ready() bool {
	return r.ready.Load()
}

// Transform executes a user-provided JS script against the given input.
// The script must define a function `transform(req)` that returns a transformed object.
//
// Returns the transform output, or an error if the script fails.
func (r *Runner) Transform(ctx context.Context, script string, input TransformInput) (*TransformOutput, error) {
	if !r.ready.Load() {
		return nil, fmt.Errorf("wasm runner not ready")
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal input: %w", err)
	}

	release, err := r.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	resultJSON, err := r.eval(ctx, "transform.js", buildProgram(script, "__th_runTransform", inputJSON))
	if err != nil {
		return nil, fmt.Errorf("exec transform: %w", err)
	}

	// M-07: Check output size limit to prevent OOM from malicious scripts.
	if len(resultJSON) > r.maxOutputSize {
		return nil, fmt.Errorf("transform output too large: %d bytes (limit %d)", len(resultJSON), r.maxOutputSize)
	}

	var result TransformOutput
	if err := json.Unmarshal([]byte(resultJSON), &result); err != nil {
		return nil, fmt.Errorf("unmarshal output: %w (raw: %s)", err, resultJSON)
	}

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
	if !r.ready.Load() {
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

	release, err := r.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	resultJSON, err := r.eval(ctx, "handler.js", buildProgram(script, "__th_runHandler", inputJSON))
	if err != nil {
		return nil, fmt.Errorf("exec handler: %w", err)
	}
	// No handler defined, or it returned null: use the default response.
	if resultJSON == "null" {
		return nil, nil
	}

	// M-07: Check output size limit to prevent OOM from malicious scripts.
	if len(resultJSON) > r.maxOutputSize {
		return nil, fmt.Errorf("handler output too large: %d bytes (limit %d)", len(resultJSON), r.maxOutputSize)
	}

	var result ResponseHandlerOutput
	if err := json.Unmarshal([]byte(resultJSON), &result); err != nil {
		return nil, fmt.Errorf("unmarshal handler output: %w (raw: %s)", err, resultJSON)
	}

	return &result, nil
}

// acquire waits for a free runtime slot, giving up when ctx is done (e.g. the
// webhook sender disconnected). The returned func releases the slot.
func (r *Runner) acquire(ctx context.Context) (func(), error) {
	select {
	case r.slots <- struct{}{}:
		return func() { <-r.slots }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("waiting for wasm runtime: %w", ctx.Err())
	}
}

// eval runs a program in a freshly set-up runtime (MED-005: no state
// leakage) and returns its value as a string. The run is cut off at
// r.timeout: qjs ignores Option.MaxExecutionTime (its interrupt handler is
// compiled out), so the deadline goes through the runtime's context, which
// wazero enforces.
func (r *Runner) eval(ctx context.Context, file, program string) (result string, err error) {
	sb, err := r.sandbox()
	if err != nil {
		return "", fmt.Errorf("create runtime: %w", err)
	}
	// reusable stays false unless the script returned normally; a runtime
	// that panicked, timed out or grew its memory is thrown away.
	reusable := false
	defer func() { r.recycle(sb, reusable) }()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	sb.ctx.set(ctx)
	defer sb.ctx.set(context.Background())

	// Start the clock once the runtime exists, so creating it (slow the
	// first time, while the module compiles) doesn't count against the script.
	var timedOut atomic.Bool
	timer := time.AfterFunc(r.timeout, func() { timedOut.Store(true); cancel() })
	defer timer.Stop()

	// qjs panics rather than returning an error when the module is closed
	// mid-run, so a timeout surfaces here.
	defer func() {
		if p := recover(); p != nil {
			reusable = false
			result = ""
			if timedOut.Load() {
				err = fmt.Errorf("script took longer than %s and was stopped", r.timeout)
			} else {
				err = fmt.Errorf("script runtime failed: %v", p)
			}
		}
	}()

	seed, err := sb.rt.Eval("seed.js", qjs.Code(seedProgram()))
	if err != nil {
		return "", fmt.Errorf("seed runtime: %w", err)
	}
	seed.Free()

	val, err := sb.rt.Eval(file, qjs.Code(program))
	if err != nil {
		if timedOut.Load() {
			return "", fmt.Errorf("script took longer than %s and was stopped", r.timeout)
		}
		// A script error leaves the runtime intact; a cancelled context
		// has closed it.
		reusable = ctx.Err() == nil
		return "", err
	}
	defer val.Free()
	reusable = true
	return val.String(), nil
}

// buildProgram follows a user script with a call to the given prelude entry
// point on the input; the program's value is the JSON result. The prelude is
// already loaded in the runtime, so line numbers in errors match the script.
func buildProgram(script, entry string, inputJSON []byte) string {
	return script + "\n;\n" + entry + "(JSON.parse(" + quote(string(inputJSON)) + "));\n"
}

// quote returns a JS string literal wrapping s, safe for embedding in JS source.
func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
