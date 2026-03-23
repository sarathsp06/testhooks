# Security Best Practices Report — Testhooks

**Date:** 2026-03-22  
**Scope:** Go backend + Svelte 5 SPA frontend  
**Auditor:** Automated security review  
**Status:** READ-ONLY AUDIT — no code changes made

---

## Executive Summary

Testhooks is a self-hostable webhook capture tool with a Go backend (single binary) and a Svelte 5 SPA frontend. The application is designed to be anonymous (no auth) and accepts arbitrary HTTP requests from the public internet, which creates an inherently large attack surface.

The codebase demonstrates several good security practices: parameterized SQL queries, body size limits on the capture path, sandboxed WASM execution with resource limits, and proper use of `crypto/rand`. However, there are significant gaps in HTTP server hardening, SSRF protection, WebSocket security, CORS configuration, and security headers.

**Finding Summary:**

| Severity | Count |
|----------|-------|
| Critical | 0     |
| High     | 3     |
| Medium   | 5     |
| Low      | 3     |
| Info     | 3     |
| **Total** | **14** |

---

## Findings

### HIGH-001: No SSRF Protection in Server-Side Forwarding

| Field | Value |
|-------|-------|
| **Severity** | HIGH |
| **Location** | `internal/forward/forwarder.go:157-189` |
| **Category** | GO-SSRF-001 |

**Description:** The server-side forwarding feature forwards webhook payloads to user-configured URLs via HTTP. There is no validation of the target URL — no scheme restriction, no blocking of private/internal IP ranges (`127.0.0.0/8`, `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `169.254.169.254`), and no DNS rebinding protection.

**Evidence:** The forward URL comes from `endpoint.config` (user-controlled data stored in Postgres). The HTTP client at line 157 issues a request to whatever URL is provided with no pre-flight validation.

**Impact:** An attacker can create an endpoint with a forward URL pointing to internal services, cloud metadata endpoints (`http://169.254.169.254/latest/meta-data/`), or other private network resources. This could expose AWS/GCP credentials, internal APIs, or Postgres on `localhost:5432`.

**Recommendation:**
1. Resolve the target URL's IP address before connecting and reject private/loopback/link-local ranges.
2. Restrict schemes to `http` and `https` only (block `file://`, `gopher://`, etc.).
3. Use a custom `net.Dialer` with a `Control` function to block connections to private IPs after DNS resolution (prevents DNS rebinding).
4. Consider a configurable allowlist/denylist for forward targets.

**Mitigation already in place:** Redirects are disabled (`CheckRedirect` returns `http.ErrUseLastResponse`), response body is capped at 1MB, and timeout is 10s. These limit the blast radius but do not prevent the initial SSRF.

---

### HIGH-002: WebSocket Accepts Connections Without Origin Validation

| Field | Value |
|-------|-------|
| **Severity** | HIGH |
| **Location** | `internal/handler/ws.go:53` |
| **Category** | WebSocket Security |

**Description:** The WebSocket upgrade uses `websocket.AcceptOptions{InsecureSkipVerify: true}`, which disables origin checking entirely. Any website can open a WebSocket connection to a Testhooks endpoint and receive live webhook data.

**Evidence:**
```go
conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
    InsecureSkipVerify: true,
})
```

**Impact:** Cross-site WebSocket hijacking (CSWSH). A malicious page visited by a user can connect to `wss://hooks.example.com/ws/<slug>` and exfiltrate webhook payloads streamed over WebSocket. Since there is no auth, knowing (or brute-forcing) a slug is sufficient.

**Recommendation:**
1. Remove `InsecureSkipVerify: true` and let the library validate the `Origin` header against the server's host.
2. Alternatively, set `OriginPatterns` to explicitly allow your SPA's origin(s).

**Note:** Since endpoints are anonymous and slugs are public, the practical impact depends on slug secrecy. But defense-in-depth still warrants origin validation.

---

### HIGH-003: Missing HTTP Server Timeouts (`ReadTimeout`, `WriteTimeout`)

| Field | Value |
|-------|-------|
| **Severity** | HIGH |
| **Location** | `cmd/testhooks/main.go:135-140` |
| **Category** | GO-HTTP-001 |

**Description:** The HTTP server sets `ReadHeaderTimeout: 10s` and `IdleTimeout: 120s`, but omits `ReadTimeout`, `WriteTimeout`, and `MaxHeaderBytes`. Without `ReadTimeout`, a client can keep a connection open indefinitely by sending a body very slowly (slowloris-style attack). Without `WriteTimeout`, a slow-reading client can hold server goroutines open indefinitely.

**Evidence:**
```go
srv := &http.Server{
    Addr:              cfg.Listen,
    Handler:           mux,
    ReadHeaderTimeout: 10 * time.Second,
    IdleTimeout:       120 * time.Second,
}
```

**Impact:** Denial of service. An attacker can exhaust server goroutines/file descriptors by opening many slow connections.

**Recommendation:**
1. Add `ReadTimeout: 30 * time.Second` (or appropriate value).
2. Add `WriteTimeout: 30 * time.Second`.
3. Add `MaxHeaderBytes: 1 << 20` (1MB) to limit header size.
4. Note: `WriteTimeout` applies to the entire response lifecycle including WebSocket connections. For endpoints that serve both WebSocket and regular HTTP, consider using `http.TimeoutHandler` as middleware for non-WS routes, or set a longer `WriteTimeout` and enforce tighter timeouts at the handler level.

---

### MED-001: Wildcard CORS Configuration

| Field | Value |
|-------|-------|
| **Severity** | MEDIUM |
| **Location** | `cmd/testhooks/main.go:125-132` |
| **Category** | CORS |

**Description:** CORS is configured with `AllowedOrigins: []string{"*"}` and `AllowedHeaders: []string{"*"}`, which allows any origin to make credentialed cross-origin requests to the API.

**Evidence:**
```go
c := cors.New(cors.Options{
    AllowedOrigins: []string{"*"},
    AllowedMethods: []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
    AllowedHeaders: []string{"*"},
})
```

**Impact:** While the application currently has no auth (so no cookies/sessions to steal), this weakens defense-in-depth. If auth is ever added, this configuration would allow any origin to make authenticated API calls. Combined with CSWSH (HIGH-002), this broadens the attack surface.

**Recommendation:**
1. In production, set `AllowedOrigins` to the actual SPA origin(s).
2. In dev mode, use `*` only if `--dev` flag is set.
3. Restrict `AllowedHeaders` to the specific headers needed (`Content-Type`, `Authorization` if auth is added).

---

### MED-002: API Request Body Size Not Limited

| Field | Value |
|-------|-------|
| **Severity** | MEDIUM |
| **Location** | `internal/handler/api.go:35, 105` |
| **Category** | GO-HTTP-002 |

**Description:** The capture handler (`capture.go:70`) correctly uses `io.LimitReader` to cap request bodies. However, the API handlers for creating and updating endpoints (`api.go:35`, `api.go:105`) decode JSON from `r.Body` without using `http.MaxBytesReader`, allowing arbitrarily large JSON payloads to be sent to these endpoints.

**Evidence:**
```go
// api.go:35 — CreateEndpoint
json.NewDecoder(r.Body).Decode(&body)

// api.go:105 — UpdateEndpoint
json.NewDecoder(r.Body).Decode(&body)
```

**Impact:** Memory exhaustion. An attacker can send a multi-gigabyte JSON payload to `/api/endpoints` and force the server to allocate large amounts of memory before parsing fails.

**Recommendation:**
```go
r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit for API payloads
```

---

### MED-003: IP Spoofing via Trusted Proxy Headers

| Field | Value |
|-------|-------|
| **Severity** | MEDIUM |
| **Location** | `internal/handler/capture.go:88-89`, `internal/middleware/ratelimit.go:125-138` |
| **Category** | Input Validation |

**Description:** Both the capture handler and the rate limiter extract client IPs from `X-Forwarded-For` and `X-Real-IP` headers without verifying the request came from a trusted proxy. Any client can set these headers to spoof their IP address.

**Evidence:**
```go
// capture.go:88-89
if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
    ip = strings.TrimSpace(strings.Split(xff, ",")[0])
}

// ratelimit.go:125-138
func getClientIP(r *http.Request) string {
    if xff := r.Header.Get("X-Forwarded-For"); xff != "" { ... }
    if xri := r.Header.Get("X-Real-IP"); xri != "" { ... }
}
```

**Impact:**
1. **Rate limit bypass:** Attacker can rotate `X-Forwarded-For` values to bypass per-IP rate limiting entirely.
2. **IP logging inaccuracy:** Captured request metadata shows fake IPs.

**Recommendation:**
1. Add a `TRUSTED_PROXIES` config option (list of CIDR ranges).
2. Only honor `X-Forwarded-For` / `X-Real-IP` when `r.RemoteAddr` matches a trusted proxy.
3. Parse `X-Forwarded-For` correctly — use the rightmost untrusted IP, not the leftmost (which is most easily spoofed).

---

### MED-004: No WebSocket Message Size Limit

| Field | Value |
|-------|-------|
| **Severity** | MEDIUM |
| **Location** | `internal/handler/ws.go:79` |
| **Category** | WebSocket Security |

**Description:** The WebSocket read loop uses `wsjson.Read` without explicitly setting `conn.SetReadLimit()`. While `nhooyr.io/websocket` has a default read limit (32768 bytes), this is not explicitly configured and may not be appropriate for all message types.

**Impact:** A malicious WebSocket client could send large messages to consume server memory. The browser-mode custom response path (`response_result` messages) receives arbitrary body content from the browser and delivers it to waiting handlers.

**Recommendation:**
1. Explicitly set `conn.SetReadLimit(maxBytes)` after accepting the connection.
2. A reasonable limit for `response_result` messages would be 1-2MB.

---

### MED-005: QuickJS Runtime Pool Reuse Without State Reset

| Field | Value |
|-------|-------|
| **Severity** | MEDIUM |
| **Location** | `internal/wasm/runner.go:163` |
| **Category** | Sandbox Isolation |

**Description:** The server-side QuickJS runtime pool reuses `qjs.Runtime` objects across different script executions via `sync.Pool`. When a runtime is returned to the pool (`pool.Put(rt)` at line 163), any global state set by the previous script may persist.

**Impact:** Cross-endpoint data leakage. If endpoint A's transform script sets a global variable, endpoint B's transform may be able to read it when it gets the same pooled runtime. This could leak sensitive webhook data between endpoints.

**Recommendation:**
1. Create a fresh `qjs.Runtime` for each execution instead of pooling (the overhead of QuickJS startup is low).
2. Alternatively, explicitly reset globals between uses — but this is fragile and hard to guarantee.
3. At minimum, document this behavior as a known limitation.

---

### LOW-001: No Content-Security-Policy Header

| Field | Value |
|-------|-------|
| **Severity** | LOW |
| **Location** | `cmd/testhooks/main.go` (missing), `web/src/app.html` (missing) |
| **Category** | CSP Headers |

**Description:** No Content-Security-Policy (CSP) header is set anywhere — not in the Go server's middleware, not in the SPA's HTML, and no SvelteKit `hooks.server.ts` file exists. The application also lacks `X-Content-Type-Options`, `X-Frame-Options`, and `Strict-Transport-Security` headers.

**Impact:** Without CSP, any XSS vulnerability would have unrestricted access to execute arbitrary JavaScript, load external resources, and exfiltrate data. Without `X-Frame-Options`, the application can be embedded in an iframe for clickjacking attacks.

**Recommendation:** Add a middleware in the Go server that sets security headers for all responses:
```go
w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com; connect-src 'self' wss:")
w.Header().Set("X-Content-Type-Options", "nosniff")
w.Header().Set("X-Frame-Options", "DENY")
w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
```

**Note:** The CSP must allow `wasm-unsafe-eval` for QuickJS WASM and `connect-src wss:` for WebSocket connections.

---

### LOW-002: Default Database URL Contains Credentials

| Field | Value |
|-------|-------|
| **Severity** | LOW |
| **Location** | `internal/config/config.go:19` |
| **Category** | Secret Management |

**Description:** The default value for `DATABASE_URL` is `postgres://testhooks:testhooks@localhost:5432/testhooks`. While this is clearly a local development default, it appears in source code and could be accidentally used in production if the environment variable is not set.

**Evidence:**
```go
DatabaseURL string `env:"DATABASE_URL" envDefault:"postgres://testhooks:testhooks@localhost:5432/testhooks"`
```

**Impact:** Low — this is a common pattern for development defaults. The credentials (`testhooks:testhooks`) are not real production secrets. However, if the application is deployed without setting `DATABASE_URL`, it would attempt to connect with these default credentials.

**Recommendation:**
1. Consider making `DATABASE_URL` required (no default) so the application fails to start without explicit configuration.
2. Alternatively, log a warning at startup if the default is being used.

---

### LOW-003: Hub Ring Buffers Not Cleaned Up on Endpoint Deletion (Memory)

| Field | Value |
|-------|-------|
| **Severity** | LOW |
| **Location** | `internal/hub/hub.go:172-176` |
| **Category** | Resource Management |

**Description:** Ring buffers for browser-mode endpoints are only cleaned up when `RemoveBuffer` is explicitly called. If an endpoint is deleted but `RemoveBuffer` is not called (or if many browser-mode endpoints are created and abandoned), ring buffers accumulate in memory.

**Impact:** Gradual memory growth on long-running servers with many browser-mode endpoints. Each ring buffer holds up to 100 messages (default) in memory.

**Recommendation:** Verify that `RemoveBuffer` is called in the endpoint deletion path. Add a periodic sweep that removes buffers for slugs that no longer exist in the database.

---

### INFO-001: All SQL Queries Use Parameterized Statements

| Field | Value |
|-------|-------|
| **Severity** | INFO (POSITIVE) |
| **Location** | `internal/db/queries.go` (all queries) |
| **Category** | GO-INJECT-001 |

**Description:** Every SQL query in `queries.go` uses parameterized placeholders (`$1`, `$2`, etc.) with `pgx`. No string concatenation or interpolation is used to build SQL. This effectively prevents SQL injection.

**Evidence:** All 13 query functions (`CreateEndpoint`, `GetEndpointBySlug`, `GetEndpointByID`, `ListEndpoints`, `UpdateEndpoint`, `DeleteEndpoint`, `InsertRequest`, `ListRequests`, `GetRequest`, `DeleteRequest`, `DeleteAllRequests`, `PruneByStorageBudget`, `PruneExcessRequests`) use `$N` placeholders.

**Assessment:** SQL injection risk is **effectively mitigated**.

---

### INFO-002: Frontend Uses Svelte Text Interpolation (XSS Mitigated)

| Field | Value |
|-------|-------|
| **Severity** | INFO (POSITIVE) |
| **Location** | `web/src/lib/components/RequestDetail.svelte`, all `.svelte` files |
| **Category** | Frontend XSS |

**Description:** The Svelte frontend renders all user-controlled data (request headers, bodies, query params, IPs, error messages) using Svelte's default text interpolation (`{variable}`), which auto-escapes HTML entities. Only one `{@html}` usage exists in the codebase (`[slug]/+page.svelte:679`), and it renders a static string literal, not user input.

**Evidence:** In `RequestDetail.svelte`, request headers are rendered at line 305-307, body at line 383, query params at line 334-336, and error messages at lines 259, 271, 489 — all via `{variable}` text interpolation.

**Assessment:** XSS risk is **effectively mitigated** by Svelte's default escaping. No `innerHTML`, `document.write`, or `eval()` usage found.

---

### INFO-003: Browser-Side WASM Sandboxing is Properly Isolated

| Field | Value |
|-------|-------|
| **Severity** | INFO (POSITIVE) |
| **Location** | `web/src/lib/wasm.ts` |
| **Category** | Client-Side Sandbox |

**Description:** Browser-side transforms use QuickJS WASM (`quickjs-emscripten`) which creates a fresh VM context per execution (`quickJSModule.newContext()` at line 68). Each context is properly disposed after use (`vm.dispose()` in `finally` block at line 127). User scripts run inside the WASM sandbox with no access to DOM, network, or host globals.

**Assessment:** Browser-side sandbox isolation is **well-implemented**. The per-request context creation prevents state leakage between executions (unlike the server-side pool — see MED-005).

---

## Overall Assessment

### Strengths
- **SQL injection:** Fully parameterized queries throughout — no risk identified
- **Frontend XSS:** Svelte's auto-escaping eliminates the primary XSS vector
- **Capture body limit:** 512KB limit enforced via `io.LimitReader`
- **Server-side WASM sandbox:** Memory limit (64MB), stack limit (1MB), execution timeout (5s)
- **Browser-side WASM sandbox:** Fresh context per execution, proper disposal
- **Secure randomness:** `crypto/rand` used for slug generation
- **Forward client hardening:** Redirects disabled, response body capped, timeout set

### Priority Remediation Order
1. **HIGH-001 (SSRF):** Most exploitable in a production deployment — can hit cloud metadata
2. **HIGH-003 (Server timeouts):** Easy to exploit for DoS, easy to fix
3. **HIGH-002 (WS origin):** Enables cross-site data exfiltration
4. **MED-003 (IP spoofing):** Undermines rate limiting entirely
5. **MED-002 (API body size):** Easy DoS vector, one-line fix
6. **MED-001 (CORS):** Defense-in-depth; critical if auth is ever added
7. **MED-005 (QuickJS pool reuse):** Data leakage risk between endpoints
8. **MED-004 (WS message size):** Explicit limits are better than defaults
9. **LOW-001 (CSP/security headers):** Defense-in-depth layer
10. **LOW-002 (Default DB URL):** Hygiene improvement

### Architecture Note
The application is intentionally designed without authentication — all endpoints are anonymous and public. This is a deliberate design decision, not a security gap. However, it means that **slug secrecy is the only access control**. If slug generation is predictable or slugs are leaked, anyone can read webhook data. The current 12-character slug with `crypto/rand` provides reasonable entropy (~71 bits assuming base62), but this should be documented as a security boundary.
