package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/sarathsp06/testhooks/internal/config"
	"github.com/sarathsp06/testhooks/internal/db"
	"github.com/sarathsp06/testhooks/internal/forward"
	"github.com/sarathsp06/testhooks/internal/hub"
	"github.com/sarathsp06/testhooks/internal/middleware"
	"github.com/sarathsp06/testhooks/internal/wasm"
)

// slugPattern validates slug format: 1-64 chars, alphanumeric + hyphens only.
var slugPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// blockedResponseHeaders are headers that user scripts must not override.
// H-02: Prevents user scripts from setting security-sensitive response headers.
var blockedResponseHeaders = map[string]bool{
	"set-cookie":                          true,
	"content-security-policy":             true,
	"content-security-policy-report-only": true,
	"x-frame-options":                     true,
	"x-content-type-options":              true,
	"strict-transport-security":           true,
	"permissions-policy":                  true,
	"referrer-policy":                     true,
	"x-xss-protection":                    true,
	"access-control-allow-origin":         true,
	"access-control-allow-credentials":    true,
	"transfer-encoding":                   true,
}

// Capture handles inbound webhook requests at /h/{slug} and /h/{slug}/{rest...}.
type Capture struct {
	db                     Store
	hub                    *hub.Hub
	forwarder              *forward.Forwarder
	wasmRunner             *wasm.Runner
	maxBodySize            int64
	browserResponseTimeout time.Duration
	trustedProxies         []*net.IPNet
	log                    zerolog.Logger
}

// NewCapture creates a new Capture handler.
func NewCapture(store Store, h *hub.Hub, fwd *forward.Forwarder, wr *wasm.Runner, maxBodySize int64, trustedProxies []*net.IPNet, log zerolog.Logger) *Capture {
	return &Capture{
		db:                     store,
		hub:                    h,
		forwarder:              fwd,
		wasmRunner:             wr,
		maxBodySize:            maxBodySize,
		browserResponseTimeout: 10 * time.Second,
		trustedProxies:         trustedProxies,
		log:                    log.With().Str("component", "capture").Logger(),
	}
}

// ServeHTTP captures any HTTP request sent to a webhook endpoint.
//
// Pipeline order: Capture → Store → Transform → Forward → Custom Response → HTTP Response
func (c *Capture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract slug from path: /h/{slug} or /h/{slug}/...
	slug := r.PathValue("slug")
	if slug == "" {
		http.Error(w, "missing slug", http.StatusBadRequest)
		return
	}
	// L-07: Validate slug format to prevent path traversal / unexpected lookups.
	if !slugPattern.MatchString(slug) {
		http.Error(w, "invalid slug format", http.StatusBadRequest)
		return
	}

	// Sub-path after the slug.
	subpath := r.PathValue("rest")
	fullPath := "/" + subpath

	// Look up endpoint.
	endpoint, err := c.db.GetEndpointBySlug(r.Context(), slug)
	if err != nil {
		c.log.Debug().Str("slug", slug).Err(err).Msg("endpoint not found")
		http.Error(w, "endpoint not found", http.StatusNotFound)
		return
	}

	// Read body (capped). L-05: Check if body was truncated and return 413.
	body, err := io.ReadAll(io.LimitReader(r.Body, c.maxBodySize+1))
	if err != nil {
		c.log.Error().Err(err).Msg("failed to read body")
		http.Error(w, "failed to read body", http.StatusInternalServerError)
		return
	}
	if int64(len(body)) > c.maxBodySize {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	// Serialize headers.
	headersJSON, _ := json.Marshal(r.Header)

	// Serialize query params.
	var queryJSON json.RawMessage
	if len(r.URL.Query()) > 0 {
		queryJSON, _ = json.Marshal(r.URL.Query())
	}

	// Client IP — use trusted-proxy-aware extraction (MED-003).
	ip := middleware.ClientIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"), r.Header.Get("X-Real-Ip"), c.trustedProxies)

	req := &db.CapturedRequest{
		EndpointID:  endpoint.ID,
		Method:      r.Method,
		Path:        fullPath,
		Headers:     headersJSON,
		Query:       queryJSON,
		Body:        body,
		ContentType: r.Header.Get("Content-Type"),
		IP:          strings.TrimSpace(ip),
		Size:        len(body),
	}

	isBrowserMode := endpoint.Mode == "browser"

	// Browser mode: generate a unique ID since we skip DB insertion (unless persist is on).
	if isBrowserMode {
		req.ID = generateRequestID()
		req.CreatedAt = time.Now()
	}

	// Parse endpoint config for server-side processing.
	epConfig := config.ParseEndpointConfig(endpoint.Config)

	// ── STEP 1: Store (server mode, or browser mode with persist_requests) ──
	if !isBrowserMode {
		if err := c.db.InsertRequest(r.Context(), req); err != nil {
			c.log.Error().Err(err).Msg("failed to store request")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	} else if epConfig.PersistRequests {
		// Browser mode with persistence: store in DB but continue with browser pipeline.
		// Use the already-generated ID and timestamp. InsertRequest will overwrite them
		// with DB-generated values which is fine — the DB ID becomes the canonical one.
		if err := c.db.InsertRequest(r.Context(), req); err != nil {
			// Non-fatal for browser mode: log and continue with WS relay.
			c.log.Warn().Err(err).Str("slug", slug).Msg("browser-mode persist failed, continuing with WS relay")
		}
	}

	// ── STEP 2: Transform ──
	// Build the forward request from the original captured data.
	fwdReq := forward.Request{
		Method:      r.Method,
		Path:        fullPath,
		Headers:     r.Header,
		Body:        body,
		ContentType: r.Header.Get("Content-Type"),
	}

	// Also track the post-transform input for the handler (starts as original).
	handlerInput := wasm.TransformInput{
		Method:      r.Method,
		Path:        fullPath,
		Headers:     r.Header,
		Query:       r.URL.Query(),
		Body:        string(body),
		ContentType: r.Header.Get("Content-Type"),
	}

	// Run WASM transform if configured (both modes — server runs it here,
	// browser mode runs it server-side too so the pipeline is consistent).
	transformApplied := false
	if epConfig.WASMScript != "" && c.wasmRunner != nil && c.wasmRunner.Ready() && !isBrowserMode {
		transformInput := wasm.TransformInput{
			Method:      r.Method,
			Path:        fullPath,
			Headers:     r.Header,
			Query:       r.URL.Query(),
			Body:        string(body),
			ContentType: r.Header.Get("Content-Type"),
		}

		transformStart := time.Now()
		output, err := c.wasmRunner.Transform(r.Context(), epConfig.WASMScript, transformInput)
		transformDur := time.Since(transformStart)

		if err != nil {
			c.log.Warn().Err(err).Str("slug", slug).Str("request_id", req.ID).
				Dur("duration", transformDur).Msg("wasm transform failed")
		} else if output != nil {
			if output.Drop {
				c.log.Info().Str("slug", slug).Str("request_id", req.ID).
					Dur("duration", transformDur).Msg("transform dropped request")
				// Publish the request to WS even if dropped (so UI shows it),
				// then return default response.
				data, _ := json.Marshal(req)
				c.hub.Publish(endpoint.Slug, hub.Message{Type: "request", Data: data}, isBrowserMode)
				c.writeDefaultResponse(w, isBrowserMode, req.ID)
				defStatus := http.StatusOK
				if isBrowserMode {
					defStatus = http.StatusAccepted
				}
				c.publishResponseInfo(slug, req.ID, defStatus, nil, defaultResponseBody(req.ID), "application/json", "default")
				return
			}

			// Apply transform output to the forward request.
			methodChanged := output.Method != "" && output.Method != fwdReq.Method
			pathChanged := output.Path != "" && output.Path != fwdReq.Path
			bodyChanged := output.Body != "" && output.Body != string(fwdReq.Body)
			headersChanged := output.Headers != nil

			if output.Body != "" {
				fwdReq.Body = []byte(output.Body)
				handlerInput.Body = output.Body
			}
			if output.ContentType != "" {
				fwdReq.ContentType = output.ContentType
				handlerInput.ContentType = output.ContentType
			}
			if output.Headers != nil {
				fwdReq.Headers = output.Headers
				handlerInput.Headers = output.Headers
			}
			if output.Method != "" {
				fwdReq.Method = output.Method
				handlerInput.Method = output.Method
			}
			if output.Path != "" {
				fwdReq.Path = output.Path
				handlerInput.Path = output.Path
			}

			transformApplied = true
			c.log.Info().Str("slug", slug).Str("request_id", req.ID).
				Dur("duration", transformDur).
				Bool("method_changed", methodChanged).
				Bool("path_changed", pathChanged).
				Bool("body_changed", bodyChanged).
				Bool("headers_changed", headersChanged).
				Msg("transform applied")
		}
	}
	_ = transformApplied // may be used for logging later

	// ── Browser mode: relay to browser via WS ──
	// The browser handles its own transform/forward/handler chain.
	if isBrowserMode {
		// Custom response + browser connected: hold request open for browser-side handler.
		if epConfig.CustomResponse != nil && epConfig.CustomResponse.Enabled && epConfig.CustomResponse.Script != "" {
			if c.hub.HasSubscribers(slug) {
				c.handleBrowserResponse(w, r, slug, req, epConfig, body)
				return
			}
			c.log.Debug().Str("slug", slug).Msg("browser-mode custom response: no subscribers, using default")
		}

		// Publish to WebSocket hub.
		data, _ := json.Marshal(req)
		c.hub.Publish(endpoint.Slug, hub.Message{Type: "request", Data: data}, true)
		c.writeDefaultResponse(w, true, req.ID)
		c.publishResponseInfo(slug, req.ID, http.StatusAccepted, nil, defaultResponseBody(req.ID), "application/json", "default")
		return
	}

	// ── Server mode continues: Forward → Custom Response → HTTP Response ──

	// Publish to WebSocket hub (so the browser UI sees it regardless of forward/handler).
	data, _ := json.Marshal(req)
	c.hub.Publish(endpoint.Slug, hub.Message{Type: "request", Data: data}, false)

	// ── STEP 3: Forward ──
	var fwdResponse *wasm.ForwardResponse // populated only for sync forwarding

	if epConfig.ForwardURL != "" && c.forwarder != nil {
		forwardMode := epConfig.ForwardMode
		if forwardMode == "" {
			forwardMode = "async"
		}

		if forwardMode == "sync" {
			// Sync: forward and capture the response.
			fwdCtx, fwdCancel := context.WithTimeout(r.Context(), 30*time.Second)
			result := c.forwarder.ForwardOne(fwdCtx, fwdReq, epConfig.ForwardURL)
			fwdCancel()

			if result.OK {
				c.log.Debug().Str("url", result.URL).Int("status", result.StatusCode).
					Int("response_size", len(result.ResponseBody)).Msg("sync forwarded")
			} else {
				c.log.Warn().Str("url", result.URL).Str("error", result.Error).
					Int("status", result.StatusCode).Msg("sync forward failed")
			}

			// Publish forward result to browser for display.
			c.publishForwardResult(slug, req.ID, result)

			// Build ForwardResponse for the handler (even on failure, so the handler can inspect it).
			respHeaders := make(map[string]string)
			for k, v := range result.ResponseHeaders {
				if len(v) > 0 {
					respHeaders[k] = v[0]
				}
			}
			fwdResponse = &wasm.ForwardResponse{
				Status:      result.StatusCode,
				Headers:     respHeaders,
				Body:        string(result.ResponseBody),
				ContentType: result.ContentType,
			}
		} else {
			// Async: fire-and-forget, don't block the webhook response.
			go func() {
				fwdCtx, fwdCancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer fwdCancel()
				result := c.forwarder.ForwardOne(fwdCtx, fwdReq, epConfig.ForwardURL)
				if result.OK {
					c.log.Debug().Str("url", result.URL).Int("status", result.StatusCode).Msg("async forwarded")
				} else {
					c.log.Warn().Str("url", result.URL).Str("error", result.Error).
						Int("status", result.StatusCode).Msg("async forward failed")
				}
				// Publish forward result to browser for display (arrives after webhook response).
				c.publishForwardResult(slug, req.ID, result)
			}()
		}
	}

	// ── STEP 4: Custom Response Handler ──
	if cr := epConfig.CustomResponse; cr != nil && cr.Enabled && cr.Script != "" {
		if c.wasmRunner != nil && c.wasmRunner.Ready() {
			handlerStart := time.Now()
			output, err := c.wasmRunner.RunResponseHandler(r.Context(), cr.Script, handlerInput, fwdResponse)
			handlerDur := time.Since(handlerStart)

			if err != nil {
				c.log.Warn().Err(err).Str("request_id", req.ID).
					Dur("duration", handlerDur).Msg("response handler script failed, using default response")
			} else if output != nil {
				c.writeCustomResponse(w, output, req.ID, handlerDur)
				// Publish response_info for the custom handler response.
				respHeaders := make(map[string]string)
				for k, v := range output.Headers {
					respHeaders[k] = v
				}
				ct := output.ContentType
				if ct == "" {
					ct = "text/plain"
				}
				status := output.Status
				if status == 0 {
					status = http.StatusOK
				}
				c.publishResponseInfo(slug, req.ID, status, respHeaders, output.Body, ct, "handler")
				return
			}
		}
	}

	// ── Sync forward without handler: return the forward target's response ──
	if fwdResponse != nil && (epConfig.CustomResponse == nil || !epConfig.CustomResponse.Enabled || epConfig.CustomResponse.Script == "") {
		// No handler configured — pass the sync forward response back to the webhook sender.
		if fwdResponse.ContentType != "" {
			w.Header().Set("Content-Type", fwdResponse.ContentType)
		}
		status := fwdResponse.Status
		if status == 0 {
			status = http.StatusBadGateway
		}
		w.WriteHeader(status)
		if fwdResponse.Body != "" {
			w.Write([]byte(fwdResponse.Body))
		}
		c.publishResponseInfo(slug, req.ID, status, fwdResponse.Headers, fwdResponse.Body, fwdResponse.ContentType, "forward_passthrough")
		return
	}

	// ── STEP 5: Default HTTP Response ──
	c.writeDefaultResponse(w, false, req.ID)
	c.publishResponseInfo(slug, req.ID, http.StatusOK, nil, defaultResponseBody(req.ID), "application/json", "default")
}

// handleBrowserResponse holds the HTTP connection open, sends a response_needed
// message to the browser via WebSocket, and waits for the browser to return
// a computed response (or times out).
func (c *Capture) handleBrowserResponse(w http.ResponseWriter, r *http.Request, slug string, req *db.CapturedRequest, epConfig config.EndpointConfig, body []byte) {
	// Register a pending response channel before publishing.
	respCh, cleanup := c.hub.WaitForResponse(slug, req.ID)
	defer cleanup()

	// Build the response_needed message with the full request data.
	type responseNeededData struct {
		RequestID string              `json:"request_id"`
		Request   *db.CapturedRequest `json:"request"`
	}
	neededData, _ := json.Marshal(responseNeededData{
		RequestID: req.ID,
		Request:   req,
	})

	// Publish response_needed to browser (not buffered — if no one is listening, it's lost).
	msg := hub.Message{
		Type: "response_needed",
		Data: neededData,
	}
	c.hub.Publish(slug, msg, false)

	// Also publish the normal "request" event so the request shows up in the UI list.
	reqData, _ := json.Marshal(req)
	reqMsg := hub.Message{
		Type: "request",
		Data: reqData,
	}
	c.hub.Publish(slug, reqMsg, true)

	// Wait for browser response or timeout.
	ctx, cancel := context.WithTimeout(r.Context(), c.browserResponseTimeout)
	defer cancel()

	select {
	case result := <-respCh:
		// Browser sent back a response — use it.
		// H-02: Block security-sensitive headers from browser responses.
		respHeaders := make(map[string]string)
		for k, v := range result.Headers {
			if blockedResponseHeaders[strings.ToLower(k)] {
				c.log.Warn().Str("header", k).Str("request_id", req.ID).Msg("blocked security-sensitive header from browser response")
				continue
			}
			w.Header().Set(k, v)
			respHeaders[k] = v
		}
		ct := result.ContentType
		if ct == "" && w.Header().Get("Content-Type") == "" {
			ct = "text/plain"
		}
		if ct != "" {
			w.Header().Set("Content-Type", ct)
			respHeaders["Content-Type"] = ct
		}
		status := result.Status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		if result.Body != "" {
			w.Write([]byte(result.Body))
		}
		c.log.Debug().Str("slug", slug).Str("request_id", req.ID).Int("status", status).Msg("browser response applied")
		c.publishResponseInfo(slug, req.ID, status, respHeaders, result.Body, ct, "handler")

	case <-ctx.Done():
		// Timeout — browser didn't respond in time. Send default.
		c.log.Warn().Str("slug", slug).Str("request_id", req.ID).Dur("timeout", c.browserResponseTimeout).Msg("browser response timed out, using default")
		c.writeDefaultResponse(w, true, req.ID)
		c.publishResponseInfo(slug, req.ID, http.StatusAccepted, nil, defaultResponseBody(req.ID), "application/json", "default")
	}
}

// writeCustomResponse sends the handler script's output as the HTTP response.
// H-02: Blocks security-sensitive response headers from user scripts.
func (c *Capture) writeCustomResponse(w http.ResponseWriter, output *wasm.ResponseHandlerOutput, reqID string, dur time.Duration) {
	for k, v := range output.Headers {
		if blockedResponseHeaders[strings.ToLower(k)] {
			c.log.Warn().Str("header", k).Str("request_id", reqID).Msg("blocked security-sensitive header from custom response")
			continue
		}
		w.Header().Set(k, v)
	}
	ct := output.ContentType
	if ct == "" && w.Header().Get("Content-Type") == "" {
		ct = "text/plain"
	}
	if ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	status := output.Status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	if output.Body != "" {
		w.Write([]byte(output.Body))
	}

	c.log.Info().Str("request_id", reqID).
		Dur("duration", dur).
		Int("status", status).
		Int("body_length", len(output.Body)).
		Int("header_count", len(output.Headers)).
		Msg("custom response applied")
}

// writeDefaultResponse sends the standard JSON response to the webhook sender.
// L-06: Uses json.Marshal instead of fmt.Sprintf to prevent injection.
func (c *Capture) writeDefaultResponse(w http.ResponseWriter, isBrowserMode bool, reqID string) {
	w.Header().Set("Content-Type", "application/json")
	status := http.StatusOK
	if isBrowserMode {
		status = http.StatusAccepted
	}
	w.WriteHeader(status)
	resp := map[string]string{"status": "ok", "id": reqID}
	data, _ := json.Marshal(resp)
	w.Write(data)
	w.Write([]byte("\n"))
}

// responseInfoPayload is the JSON shape for a response_info WS message.
type responseInfoPayload struct {
	RequestID   string            `json:"request_id"`
	Status      int               `json:"status"`
	Headers     map[string]string `json:"headers,omitempty"`
	Body        string            `json:"body,omitempty"`
	ContentType string            `json:"content_type,omitempty"`
	Source      string            `json:"source"` // "default", "handler", "forward_passthrough"
}

// publishResponseInfo sends a response_info message over the WS hub so the
// browser can display the HTTP response that was sent back to the webhook caller.
func (c *Capture) publishResponseInfo(slug string, reqID string, status int, headers map[string]string, body string, contentType string, source string) {
	payload := responseInfoPayload{
		RequestID:   reqID,
		Status:      status,
		Headers:     headers,
		Body:        body,
		ContentType: contentType,
		Source:      source,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		c.log.Warn().Err(err).Str("request_id", reqID).Msg("failed to marshal response_info")
		return
	}
	c.hub.Publish(slug, hub.Message{Type: "response_info", Data: data}, false)
}

// forwardResultPayload is the JSON shape for a forward_result WS message.
type forwardResultPayload struct {
	RequestID       string            `json:"request_id"`
	URL             string            `json:"url"`
	StatusCode      int               `json:"status_code"`
	OK              bool              `json:"ok"`
	LatencyMs       int64             `json:"latency_ms"`
	Error           string            `json:"error,omitempty"`
	ResponseBody    string            `json:"response_body,omitempty"`
	ResponseHeaders map[string]string `json:"response_headers,omitempty"`
	ContentType     string            `json:"content_type,omitempty"`
}

// publishForwardResult sends a forward_result message over the WS hub so the
// browser can display the forward target's response.
func (c *Capture) publishForwardResult(slug string, reqID string, result forward.Result) {
	respHeaders := make(map[string]string)
	for k, v := range result.ResponseHeaders {
		if len(v) > 0 {
			respHeaders[k] = v[0]
		}
	}

	payload := forwardResultPayload{
		RequestID:       reqID,
		URL:             result.URL,
		StatusCode:      result.StatusCode,
		OK:              result.OK,
		LatencyMs:       result.Latency.Milliseconds(),
		Error:           result.Error,
		ResponseBody:    string(result.ResponseBody),
		ResponseHeaders: respHeaders,
		ContentType:     result.ContentType,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		c.log.Warn().Err(err).Str("request_id", reqID).Msg("failed to marshal forward_result")
		return
	}
	c.hub.Publish(slug, hub.Message{Type: "forward_result", Data: data}, false)
}

// defaultResponseBody returns a safe JSON response body string. L-06.
func defaultResponseBody(reqID string) string {
	data, _ := json.Marshal(map[string]string{"status": "ok", "id": reqID})
	return string(data)
}

// GenerateSlug creates a random 8-character hex slug.
func GenerateSlug() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// generateRequestID creates a random UUID v4-style identifier for browser-mode
// requests that bypass DB insertion (which would normally generate the ID).
func generateRequestID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 1
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
