# Testhooks — Security & Licensing Audit Report

**Date:** March 26, 2026
**Scope:** Full codebase — Go backend, Svelte 5 frontend, Docker deployment, dependencies, licensing
**Methodology:** Manual code audit against Go backend security best practices (OWASP, Go security spec) and JavaScript frontend security best practices (OWASP DOM XSS prevention, CSP, third-party controls)

---

## Executive Summary

Testhooks is a well-engineered webhook capture tool with several security strengths: parameterized SQL queries (no injection), robust SSRF protection on server-side forwarding, WASM-sandboxed user script execution, a minimal `scratch` Docker image, and proper HTTP server timeouts. All dependency licenses are permissive and compatible.

However, the **intentional lack of authentication** is the dominant security concern — every endpoint, request, and configuration is accessible to any anonymous user. Beyond that design choice, the audit identified **5 High**, **12 Medium**, and **14 Low/Informational** findings across the stack. The most actionable fixes are: adding a `.dockerignore`, hardening CSP (removing `unsafe-inline`), limiting WebSocket connections, bounding the endpoint listing API, and sanitizing user-script response headers.

---

## Findings by Severity

### CRITICAL (1)

#### C-01: No Authentication — Full Anonymous Access to All Data
- **Location:** `internal/handler/api.go` (entire file)
- **Evidence:** All REST endpoints (`GET/POST/PATCH/DELETE /api/endpoints/*`, `/api/requests/*`) and WebSocket (`/ws/:slug`) require no authentication.
- **Impact:** Anyone can: list all endpoints (enumeration), read all captured webhook payloads (data breach — payloads often contain API keys, tokens, and secrets), delete any endpoint/request (DoS), modify any endpoint config including forward URLs (redirect webhooks to attacker servers) and inject malicious WASM scripts.
- **Status:** By design ("Auth: Skipped by design"). Acceptable for personal/localhost use. **Unacceptable for any internet-facing deployment.**
- **Fix:** If the app will be public-facing, implement at minimum API key or bearer token authentication scoped per endpoint. Short-term: restrict `ListEndpoints` to return only the caller's endpoints; require a secret token (generated at creation) for mutation operations.

---

### HIGH (5)

#### H-01: Default Database URL Has Embedded Credentials
- **Location:** `internal/config/config.go:23`
- **Evidence:** `envDefault:"postgres://testhooks:testhooks@localhost:5432/testhooks?sslmode=disable"`
- **Impact:** If `DATABASE_URL` is not set in production, the app uses well-known credentials. The current code only warns (`log.Warn`) instead of failing.
- **Fix:** Change `log.Warn()` at `config.go:104` to `log.Fatal()` when the default DB URL is used in non-dev mode. Also: default `sslmode` should be `require` for production.

#### H-02: User Scripts Can Override Security Response Headers
- **Location:** `internal/handler/capture.go:432-434` (server-mode custom response), `capture.go:399-401` (browser-mode response)
- **Evidence:**
  ```go
  for k, v := range output.Headers {
      w.Header().Set(k, v)
  }
  ```
- **Impact:** A WASM script or browser-mode WebSocket client can set `Set-Cookie` (session fixation), `Content-Security-Policy` (weaken XSS defense), `Location` (open redirect), or `X-Frame-Options` (enable clickjacking). The security middleware runs *before* the handler, so `w.Header().Set()` in the handler overwrites security headers.
- **Fix:** Blocklist security-sensitive headers from user-script output: `Set-Cookie`, `Content-Security-Policy`, `X-Frame-Options`, `X-Content-Type-Options`, `Strict-Transport-Security`, `Access-Control-*`, `Referrer-Policy`.

#### H-03: `ListEndpoints` Has No Pagination — Unbounded Query
- **Location:** `internal/db/queries.go:159-162`, `internal/handler/api.go:72-83`
- **Evidence:** `SELECT ... FROM endpoints ORDER BY created_at DESC` — no `LIMIT` clause.
- **Impact:** An attacker creates thousands of endpoints, then calls `GET /api/endpoints` to force the server to serialize a massive JSON response, consuming memory and CPU (DoS). The DB query also scans the full table.
- **Fix:** Add pagination (limit/offset) to `ListEndpoints`, matching the existing `ListRequests` pattern (max 200 per page).

#### H-04: Ring Buffers in Hub Never Auto-Cleaned
- **Location:** `internal/hub/hub.go:148-151`
- **Evidence:** Ring buffers are created per browser-mode slug on first `Publish(useBuffer=true)` but are only removed by explicit `RemoveBuffer(slug)` call. No TTL sweep exists.
- **Impact:** Abandoned browser-mode endpoints accumulate ring buffers indefinitely. Each buffer holds up to 100 messages × 512 KB = ~50 MB. Thousands of abandoned slugs = multi-GB memory leak, leading to OOM.
- **Fix:** Add a TTL-based sweep (e.g., remove buffers with no subscribers and no writes for >1 hour). Tie buffer lifecycle to endpoint deletion.

#### H-05: Missing `.dockerignore`
- **Location:** Project root (file absent)
- **Evidence:** `COPY . .` in `Dockerfile` stage 2 sends the entire directory to the Docker build context, including `.env`, `.git/`, `web/node_modules/`, IDE files.
- **Impact:** If a `.env` file with real credentials exists locally, it's copied into the build layer. Even in multi-stage builds, the build context is visible in intermediate layers if pushed to a registry.
- **Fix:** Create `.dockerignore`:
  ```
  .env
  .env.*
  .git
  web/node_modules
  web/dist
  *.md
  .idea
  .vscode
  ```

---

### MEDIUM (12)

#### M-01: CSP `script-src 'unsafe-inline'` Weakens XSS Protection
- **Location:** `internal/middleware/security.go:33`
- **Evidence:** `script-src 'self' 'unsafe-inline' 'wasm-unsafe-eval'`
- **Impact:** `unsafe-inline` largely defeats CSP's XSS protection — any HTML injection that reaches the browser can execute inline scripts.
- **Fix:** Use nonce-based CSP (`'nonce-<random>'`) or hash-based CSP for the SvelteKit bootstrap script. Remove `unsafe-inline`.

#### M-02: CORS Default `AllowedOrigins: *` + `AllowedHeaders: *`
- **Location:** `internal/config/config.go:57`, `cmd/testhooks/main.go:136`
- **Evidence:** Default CORS is `*` (all origins), `AllowedHeaders: ["*"]` (all headers).
- **Impact:** Any website can make full cross-origin API calls to create/delete endpoints and requests. Combined with no auth (C-01), this means drive-by CSRF-like attacks from any page.
- **Fix:** Default to a restrictive origin list (e.g., `["http://localhost:5173"]` for dev). In production, require explicit `ALLOWED_ORIGINS` configuration. Restrict `AllowedHeaders` to `["Content-Type"]`.

#### M-03: No Rate Limiting on WebSocket Connections
- **Location:** `cmd/testhooks/main.go:100` (rate limiter only wraps `/h/{slug}`), `internal/handler/ws.go:56`
- **Impact:** An attacker can open thousands of WebSocket connections, exhausting file descriptors and memory (each connection = goroutine + 64-message channel buffer).
- **Fix:** Add per-IP connection limits for WebSocket. Apply the existing rate limiter to `/ws/{slug}` as well, or add a separate connection counter.

#### M-04: Cross-Slug Response Delivery via WebSocket
- **Location:** `internal/handler/ws.go:103`, `internal/hub/hub.go:197`
- **Evidence:** `DeliverResponse(requestID, result)` is not scoped to the slug the WebSocket client subscribed to.
- **Impact:** A WebSocket client on `/ws/slug-A` can deliver a forged response for any request ID (e.g., a request on `/ws/slug-B`), if they know the UUID. UUIDs are hard to guess but are visible in API responses and WebSocket messages.
- **Fix:** Scope `DeliverResponse` to verify the request ID belongs to the subscribing slug.

#### M-05: No WriteTimeout on Non-WebSocket HTTP Routes
- **Location:** `cmd/testhooks/main.go:143-144`
- **Evidence:** `WriteTimeout` is intentionally omitted (would kill WebSocket). No per-route `http.TimeoutHandler` is applied to `/api/*`.
- **Impact:** A slow-read client can hold a goroutine indefinitely on REST API responses.
- **Fix:** Wrap `/api/*` handlers with `http.TimeoutHandler(handler, 30*time.Second, "timeout")`.

#### M-06: Unbounded Response Body Drain in Async Forward Path
- **Location:** `internal/forward/forwarder.go:332`
- **Evidence:** `io.Copy(io.Discard, resp.Body)` — no `LimitReader`.
- **Impact:** A malicious forward target can send an infinite response, tying up the goroutine indefinitely.
- **Fix:** `io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))` (match the 1 MB limit in `doForwardCapture`).

#### M-07: No Output Size Limit on QuickJS WASM Results
- **Location:** `internal/wasm/runner.go:172, 257`
- **Evidence:** `resultJSON := val.String()` — no size check.
- **Impact:** A malicious script can return `"x".repeat(60_000_000)` (~60 MB string within the 64 MB QuickJS memory limit), which is then copied into Go's heap.
- **Fix:** Check `len(resultJSON)` before unmarshalling; reject if > 1 MB.

#### M-08: IPv6 Rate Limit Bypass via Non-Canonical Representation
- **Location:** `internal/middleware/clientip.go:45`
- **Evidence:** IPs extracted from `X-Forwarded-For` are returned as raw strings without `net.ParseIP().String()` normalization.
- **Impact:** The same IPv6 client can get multiple rate limit buckets if the trusted proxy forwards different representations of the same address.
- **Fix:** Normalize via `net.ParseIP(ip).String()` before returning from `ClientIP()`.

#### M-09: No Subscriber Limit Per Slug in Hub
- **Location:** `internal/hub/hub.go:107-111`
- **Impact:** Unlimited WebSocket connections per slug, each allocating a goroutine and 64-message buffered channel. Memory exhaustion vector.
- **Fix:** Enforce a max subscriber count per slug (e.g., 50). Reject new subscribers when the limit is reached.

#### M-10: `pending` Map Entries Can Leak
- **Location:** `internal/hub/hub.go:182-193`
- **Impact:** If the cleanup function from `WaitForResponse` is not called (e.g., goroutine panic), the `pending` map entry and channel leak forever.
- **Fix:** Add a TTL-based sweep for stale `pending` entries (e.g., older than `browserResponseTimeout`).

#### M-11: Shell Injection in cURL Copy Feature (Header Values)
- **Location:** `web/src/lib/components/RequestDetail.svelte:164`
- **Evidence:** Header keys/values interpolated into shell command string without escaping:
  ```ts
  cmd += ` \\\n  -H '${key}: ${val}'`;
  ```
- **Impact:** A webhook sender crafts a header like `X-Evil: '; rm -rf / #`. When a user copies the cURL and pastes into a terminal, arbitrary commands execute.
- **Fix:** Apply the same `replace(/'/g, "'\\''")` escaping used for the body (line 168) to header keys and values.

#### M-12: No Execution Timeout on Browser-Side WASM Transforms
- **Location:** `web/src/lib/wasm.ts:61-128` (JS), `wasm.ts:239` (Lua), `wasm.ts:370` (Jsonnet)
- **Impact:** A user script with `while(true){}` hangs the browser tab indefinitely.
- **Fix:** Use QuickJS's `setInterruptHandler()` with a 5-second deadline. Wrap Lua/Jsonnet in `Promise.race()` with a timeout.

---

### LOW / INFORMATIONAL (14)

| ID | Finding | Location | Note |
|---|---|---|---|
| L-01 | CSP `connect-src` allows all origins | `security.go:35` | Trade-off for browser-side forwarding |
| L-02 | Missing `Permissions-Policy` header | `security.go` | Add `camera=(), microphone=(), geolocation=()` |
| L-03 | Dev reverse proxy if `DEV=true` in prod | `main.go:119` | Requires explicit misconfiguration |
| L-04 | No validation on `MaxBodySize`/`RingBufferSize` | `config.go:32,45` | Zero/negative values cause unexpected behavior |
| L-05 | Silent body truncation (no 413) on capture | `capture.go:74` | `LimitReader` truncates silently |
| L-06 | JSON built via `fmt.Sprintf` | `capture.go:467` | Fragile; use `json.Marshal` |
| L-07 | No slug format validation | `capture.go:55` | Enforce `^[0-9a-f]{1,12}$` |
| L-08 | No input length validation on endpoint name | `api.go:35` | ~1 MB name could pass |
| L-09 | `r.ready` bool not thread-safe | `wasm/runner.go:119` | Use `atomic.Bool` |
| L-10 | Unbounded rate limit client map | `ratelimit.go:79` | Add hard cap on map size |
| L-11 | `Close()` panics on double call | `ratelimit.go:66` | Use `sync.Once` |
| L-12 | Pruning queries are full table scans | `queries.go:266-293` | Process per-endpoint with batching |
| L-13 | `target="_blank"` without `rel="noopener"` | `routes/+page.svelte:286,300,1049` | Modern browsers auto-apply, best practice gap |
| L-14 | LICENSE appendix not customized | `LICENSE` | Fill in `[yyyy]` and `[name of copyright owner]` |

---

## Licensing Compliance

### Project License
- **Apache License 2.0** — valid, permissive.
- **Issue:** Appendix placeholders (`[yyyy]`, `[name of copyright owner]`) not filled in. Cosmetic but should be fixed.

### Dependency License Compatibility

| Dependency | License | Compatible with Apache-2.0? |
|---|---|---|
| All Go deps (pgx, zerolog, cors, qjs, migrate, etc.) | MIT / ISC / Apache-2.0 | Yes |
| All Node deps (Svelte, CodeMirror, Tailwind, QuickJS-emscripten, wasmoon, etc.) | MIT / ISC / Apache-2.0 | Yes |
| nhooyr.io/websocket | ISC | Yes |
| tetratelabs/wazero (indirect) | Apache-2.0 | Yes |

**No license incompatibilities found.** All dependencies use permissive licenses fully compatible with Apache-2.0.

---

## Supply Chain & CI

| Check | Status | Recommendation |
|---|---|---|
| `go.sum` committed | PASS | Good |
| `package-lock.json` committed | PASS | Good |
| `govulncheck` in CI | FAIL | No CI pipeline exists. Add GitHub Actions with `govulncheck`. |
| `npm audit` in CI | FAIL | No CI pipeline exists. Add `npm audit` step. |
| SAST/linting in CI | FAIL | No CI pipeline. Add `staticcheck` or `golangci-lint`. |
| Dependency update automation | FAIL | No Dependabot/Renovate configured. |
| `.gitignore` covers secrets | PASS | `.env` and `.env.local` are ignored. |
| No secrets in repo | PASS | No committed secrets found. |

---

## Docker / Deployment

| Check | Status | Detail |
|---|---|---|
| Multi-stage build | PASS | 3 stages (node, go, scratch) |
| Scratch final image | PASS | Minimal attack surface |
| CGO disabled | PASS | `CGO_ENABLED=0` — static binary |
| Binary stripped | PASS | `-trimpath -ldflags="-s -w"` |
| `.dockerignore` | **FAIL** | Missing — `.env` and `.git/` sent to build context |
| docker-compose Postgres exposed | WARNING | `5432:5432` on all interfaces; bind to `127.0.0.1` |
| docker-compose hardcoded credentials | WARNING | `POSTGRES_PASSWORD: testhooks` — mark as dev-only |

---

## Positive Findings (What's Done Well)

1. **No SQL injection** — all queries use parameterized placeholders (`$1`, `$2`, ...) via pgx.
2. **Robust SSRF protection** — post-DNS-resolution IP checking, private range blocking, cloud metadata IP blocking, scheme validation, redirect suppression.
3. **WASM sandbox isolation** — QuickJS runs in Wazero WASM VM with memory limit (64 MB), stack limit (1 MB), execution timeout (5s), and fresh runtime per execution.
4. **HTTP server hardened** — `ReadTimeout`, `ReadHeaderTimeout`, `IdleTimeout`, `MaxHeaderBytes` all set.
5. **Body size limits** — `MaxBytesReader` on API mutations, `LimitReader` on capture handler.
6. **Security headers** — `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, and CSP all set.
7. **Slug entropy** — 12-char hex via `crypto/rand` (48 bits of entropy).
8. **No `eval`/`innerHTML`/`document.write`** in frontend code.
9. **Minimal Docker image** — `scratch` base, no shell, no OS.
10. **All dependencies permissively licensed** — no compliance issues.

---

## Recommended Fix Priority

### Immediate (before any public deployment)
1. **H-05:** Create `.dockerignore`
2. **H-02:** Blocklist security-sensitive response headers from user scripts
3. **M-02:** Change default CORS to restrictive; require explicit `ALLOWED_ORIGINS`
4. **M-11:** Sanitize cURL header output (shell injection)
5. **H-01:** Fail hard on default DB credentials in non-dev mode

### Short-term (next sprint)
6. **H-03:** Add pagination to `ListEndpoints`
7. **M-03 + M-09:** Rate limit / cap WebSocket connections
8. **H-04:** Add TTL sweep for ring buffers
9. **M-06:** Bound async forward response drain
10. **M-07:** Limit QuickJS output size

### Medium-term
11. **M-01:** Replace `unsafe-inline` CSP with nonce-based CSP
12. **M-05:** Add `http.TimeoutHandler` to `/api/*` routes
13. **M-08:** Normalize IPv6 in `ClientIP()`
14. **M-04:** Scope `DeliverResponse` to subscribing slug
15. Set up CI with `govulncheck`, `npm audit`, `staticcheck`

### If going public-facing
16. **C-01:** Implement authentication (API keys or OAuth per endpoint)
17. Add HSTS header (when deployed behind TLS)
18. Configure Dependabot/Renovate for dependency updates
