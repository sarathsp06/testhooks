# Testhooks

A lightweight, self-hostable webhook.site replacement.

Capture, inspect, transform, and forward webhooks in real time.

---

## Core Concept

You get a unique URL (e.g. `https://hooks.example.com/h/<id>`). Any HTTP request
sent to that URL is captured and streamed live to your browser via WebSocket.
From the browser you can inspect headers/body, run JavaScript transforms (via WASM),
and fan-out / forward the request to other URLs — including `localhost` (the browser
itself makes the outbound fetch, so local targets work).

### Endpoint Modes

Each endpoint operates in one of two modes, chosen at creation time and changeable later:

| Mode              | Data stored on server? | WASM runs where | Forwarding from | Best for |
|-------------------|------------------------|-----------------|-----------------|----------|
| **Server mode**   | Yes (Postgres)         | Server (QuickJS via fastschema/qjs) | Server (Go HTTP) | Production webhooks, always-on pipelines, persistence |
| **Browser mode**  | No — pass-through only | Browser (QuickJS WASM) | Browser (fetch) | Live testing, GDPR/privacy, localhost targets |

**Server mode** is the default. The Go server stores every request in Postgres,
runs WASM transforms via Wazero, and forwards to configured URLs — all headless.
The browser is just a viewer.

**Browser mode** is the privacy-first option. The server acts as a thin relay:
it receives the webhook, streams it over WebSocket to the connected browser, and
**discards it immediately** — nothing is written to the database. All processing
happens in the browser: WASM transforms run via QuickJS, forwarding uses `fetch()`,
and if the user wants to keep request history, it goes to IndexedDB (browser-local
storage). Payloads never touch the server's disk.

This is ideal for:
- **GDPR / data residency:** Sensitive payloads never leave the user's machine.
- **Live local testing:** Forward to `localhost` while inspecting in real time.
- **Zero-storage hosting:** The server needs no database rows for these endpoints,
  reducing hosting cost and cleanup burden.

The trade-off is obvious: if the browser tab is closed, webhooks are lost (or
buffered briefly on the server and dropped after a short timeout). For reliable
capture, use server mode.

---

## Tech Stack

| Layer       | Choice                | Rationale                                         |
|-------------|-----------------------|---------------------------------------------------|
| Backend     | **Go** (single binary)| Fast, tiny memory footprint, stdlib HTTP + WS     |
| Frontend    | **Svelte 5** (SPA)    | Small bundle, reactive, compiles away              |
| Database    | **PostgreSQL**        | Reliable, JSONB for request payloads, easy to host |
| Pub/Sub     | **Redis** (optional)  | Real-time fan-out across multiple Go instances; skip if single-node |
| Real-time   | **WebSockets**        | Native Go (`nhooyr.io/websocket`), browser-native  |
| WASM (server)| **QuickJS** (`fastschema/qjs`)| QuickJS pool for server-side JS transforms; runs in capture pipeline |
| WASM (browser)| **QuickJS-WASM**    | In-browser JS/Lua/Jsonnet transforms; zero server cost for interactive use |
| Auth          | *Skipped*           | No auth — all endpoints are anonymous                |

### Why this is thin
- Single Go binary serves API + static SPA files. No Node runtime in prod.
- PostgreSQL is the only required dependency. Redis is optional (only needed for
  multi-instance pub/sub).
- No object storage — request payloads stored as JSONB in Postgres (with a
  configurable TTL / max-rows-per-endpoint to keep storage bounded).

---

## Data Model (PostgreSQL)

```sql
-- An endpoint is a unique webhook URL
CREATE TABLE endpoints (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug        VARCHAR(12) UNIQUE NOT NULL,       -- short URL token
    name        TEXT,                                -- optional label
    mode        VARCHAR(10) NOT NULL DEFAULT 'server', -- 'server' or 'browser'
    owner_id    UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ DEFAULT now(),
    config      JSONB DEFAULT '{}'                   -- forward_url, wasm_script, custom_response, etc.
);

-- Every inbound request captured
CREATE TABLE requests (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    endpoint_id UUID NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    method      VARCHAR(10) NOT NULL,
    path        TEXT NOT NULL DEFAULT '/',
    headers     JSONB NOT NULL,
    query       JSONB,
    body        BYTEA,                               -- raw body (capped at e.g. 512KB)
    content_type TEXT,
    ip          TEXT,
    size        INT,
    created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX idx_requests_endpoint ON requests (endpoint_id, created_at DESC);

-- Users (added when auth is enabled)
CREATE TABLE users (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email       TEXT UNIQUE NOT NULL,
    name        TEXT,
    avatar_url  TEXT,
    provider    TEXT DEFAULT 'google',
    created_at  TIMESTAMPTZ DEFAULT now()
);
```

**Storage discipline:**
- Background goroutine prunes requests when an endpoint exceeds its storage budget
  (default `MAX_ENDPOINT_STORAGE_BYTES` = 10 MB) or max count per endpoint (default 500).
- `body` column capped at 512 KB per request; larger payloads truncated with a flag.

---

## Architecture

```
                         ┌─────────────────────────────────┐
                         │         Browser (Svelte 5)       │
                         │                                  │
                         │  ┌──────────┐  ┌──────────────┐ │
   webhook sender ──────►│  │ Inspect  │  │ JS Transform │ │
         │               │  │ Request  │  │ (in-browser  │ │
         │               │  └──────────┘  │  WASM or eval)│ │
         │               │       ▲        └──────┬───────┘ │
         │               │       │ WS             │         │
         │               │       │          fetch()         │
         │               │       │          to localhost    │
         │               └───────┼──────────┬───────────────┘
         │                       │          │
         ▼                       │          ▼
   ┌───────────┐          ┌──────┴──────┐   local dev
   │  Go API   │◄─────────│  WebSocket  │   server / other
   │  Server   │──────────│  Hub        │   services
   │           │          └─────────────┘
   │ /h/:slug  │  capture
   │ /api/*    │  ────────► PostgreSQL
   │ /ws/:slug │
   └───────────┘
        │
        ▼ (optional, multi-instance)
      Redis Pub/Sub
```

### Request Flow

**Server-mode endpoint:**

1. **Capture:** `POST /h/:slug` → Go handler reads method, headers, query, body →
   inserts into `requests` table.
2. **Server-side WASM transform:** If the endpoint has a WASM script configured,
   QuickJS (via `fastschema/qjs` pool) executes it immediately in the capture pipeline.
   The script receives the request as JSON and returns a transformed payload. This runs
   on every inbound request regardless of whether a browser is connected — headless,
   always-on, zero user interaction needed.
3. **Server-side forwarding:** If the endpoint has a server-side forward URL configured,
   the Go server forwards the (optionally transformed) payload to that URL via HTTP.
   This is reliable — works even when no browser is open. Only public URLs (no
   localhost). Sync or async mode configurable per endpoint.
4. **Publish:** The captured request (+ transform result if any) is published to the
   in-memory hub (or Redis for multi-node).
5. **Stream:** Browser connects `WS /ws/:slug` → receives new requests in real time
   as JSON. Browser can also run its own transforms/forwards on top.

**Browser-mode endpoint:**

1. **Receive:** `POST /h/:slug` → Go handler reads method, headers, query, body →
   **does NOT write to Postgres.**
2. **Relay:** The request is published directly to the WebSocket hub. If no browser
   is connected, the server buffers briefly (configurable, e.g. 30s) then drops.
3. **Stream:** Browser receives the raw request over WebSocket.
4. **Browser-side WASM transform:** The SPA runs the user's JS script via QuickJS
   WASM entirely in-browser. All transform logic is local.
5. **Browser-side forwarding:** The browser `fetch()`es to configured targets —
   including `localhost`. Status per target shown in UI.
6. **Local persistence (optional):** If the user wants to keep history, the browser
   stores requests in IndexedDB. The server never sees them again.

**Two transform modes, two forward modes — by design:**

| Mode         | Runs where | When                       | Localhost? | Always-on? |
|--------------|------------|----------------------------|------------|------------|
| Server WASM  | Go/QuickJS | Every inbound request      | No         | Yes        |
| Browser WASM | QuickJS    | When browser tab is open   | N/A        | No         |
| Server fwd   | Go HTTP    | Every inbound request      | No         | Yes        |
| Browser fwd  | fetch()    | When browser tab is open   | Yes        | No         |

Users configure both independently. Typical flow: server WASM transforms + server
forwards for production reliability, browser WASM + browser forwards for local dev.

### How Data Reaches the Browser

Webhooks are sent by external systems to the Go server's public URL — the browser
is never directly reachable. The server bridges the gap:

```
External system                          Browser
     │                                      │
     │  POST /h/:slug                       │  WS /ws/:slug (outbound connection)
     │─────────────────► Go Server ─────────│──────────────► push over WebSocket
     │                   (public URL)        │
```

The browser maintains an **outbound** WebSocket connection to the server. The server
pushes new requests down that connection in real time. This works through NATs,
firewalls, corporate networks — the browser initiates the connection, so no inbound
ports need to be open.

**Delivery guarantees per mode:**

| Scenario                        | Server mode                              | Browser mode                                |
|---------------------------------|------------------------------------------|---------------------------------------------|
| Browser connected               | Stored in DB + pushed via WS             | Pushed via WS immediately, server discards  |
| Browser disconnects temporarily | Safe in DB; browser fetches history via REST on reconnect | Server buffers in-memory (configurable, default 30s ring buffer per slug), then drops |
| Browser never connected         | Safe in DB indefinitely (until TTL prune)| Dropped after buffer timeout — this is the explicit trade-off |
| Multiple browser tabs           | All tabs receive via WS; DB is source of truth | All connected tabs receive; first-come-first-served for forwarding |

**Browser-mode buffering detail:** The server keeps a small per-endpoint ring buffer
(default 100 requests, configurable via `RING_BUFFER_SIZE`). When a browser connects,
the buffer is flushed to it. This covers brief disconnects (page reload, network blip)
without writing anything to disk. If the buffer overflows, oldest requests are dropped
silently — the server returns `202 Accepted` to the webhook sender regardless.

---

## Go Server — Key Modules

```
cmd/
  testhooks/main.go           -- entry point, config, serve

internal/
  config/config.go           -- env/flag parsing
  db/db.go                   -- pgx queries
  db/migrations/             -- SQL migration files
  handler/
    store.go                 -- consumer-side Store interface
    capture.go               -- POST/PUT/GET/etc /h/:slug — capture inbound
    api.go                   -- REST: CRUD endpoints, list requests
    ws.go                    -- WebSocket upgrade + hub
  hub/
    hub.go                   -- in-memory pub/sub (goroutine + channels + ring buffer)
  forward/
    forwarder.go             -- server-side HTTP forwarding
  wasm/
    runner.go                -- QuickJS pool (fastschema/qjs) for server-side transforms
  cleanup/
    pruner.go                -- background storage-budget + max-count cleanup
  middleware/
    ratelimit.go             -- per-IP token-bucket rate limiting

web/                         -- Svelte 5 SPA (built at compile time, embedded via go:embed)
```

**Go Dependencies:**

| Package | Purpose | Notes |
|---------|---------|-------|
| `github.com/jackc/pgx/v5` | Postgres driver + pool | Best Go Postgres driver; `pgxpool` for connection pooling |
| `nhooyr.io/websocket` | WebSocket server | Lightweight, context-aware, no CGO |
| `github.com/fastschema/qjs` | QuickJS pool | Server-side JS transforms; pooled QuickJS instances via Wazero (indirect dep) |
| `github.com/redis/go-redis/v9` | Redis client | Optional — only for multi-instance pub/sub |
| `github.com/golang-migrate/migrate/v4` | DB migrations | Run SQL migration files from `internal/db/migrations/` |
| `github.com/rs/zerolog` | Structured logging | Zero-allocation JSON logger |
| `github.com/caarlos0/env/v11` | Config from env vars | Parses env vars into config struct with defaults |
| `github.com/rs/cors` | CORS middleware | Needed for SPA dev proxy and cross-origin API access |
| `golang.org/x/crypto` | Secure random slugs | `crypto/rand` for slug generation (stdlib may suffice) |
| Standard library (`net/http`) | HTTP routing | Go 1.22+ `net/http` mux patterns (`GET /api/...`), no framework |

No framework. Standard library HTTP mux with `net/http` route patterns.
`sqlc` is considered for type-safe query generation but not required — raw `pgx` queries are fine for this scope.

| `github.com/joho/godotenv` | `.env` file loading | Loads `.env` at startup before config parsing |

**Frontend Dependencies (Svelte 5 / TypeScript):**

| Package | Purpose | Notes |
|---------|---------|-------|
| `svelte` + `@sveltejs/kit` | Framework + routing | SvelteKit in SPA mode (`adapter-static`); file-based routing, SSR disabled |
| `quickjs-emscripten` | Browser-side WASM JS sandbox | By justjake — TypeScript wrapper around QuickJS compiled to WASM (~400KB gzipped). Sandboxed execution, no `eval()` |
| `@codemirror/view` + `@codemirror/state` | Code editor | CodeMirror 6 — modular, framework-agnostic. Use `@codemirror/lang-javascript` for JS syntax |
| `idb` | IndexedDB wrapper | By Jake Archibald — thin Promise-based wrapper over IndexedDB. For browser-mode request history |
| `bits-ui` | Headless UI primitives | Svelte 5 compatible headless components (dialog, popover, tabs, etc.). Style with Tailwind |
| `tailwindcss` | Utility CSS | Rapid styling, small production bundle with purging |
| `clsx` + `tailwind-merge` | Class name utils | Conditional + dedup Tailwind classes |
| `lucide-svelte` | Icons | Tree-shakeable SVG icon set |
| `mode-watcher` | Dark mode | Svelte 5 compatible theme toggle (system/light/dark) |
| `@sveltejs/adapter-static` | Build adapter | Outputs static SPA for `go:embed` |

**Dev Dependencies:**

| Package | Purpose | Notes |
|---------|---------|-------|
| `vite` | Bundler | Ships with SvelteKit |
| `typescript` | Type checking | Strict mode |
| `prettier` + `prettier-plugin-svelte` | Formatting | Consistent code style |
| `eslint` + `eslint-plugin-svelte` | Linting | Catch issues early |

**Why SvelteKit (SPA mode) instead of plain Svelte + Vite:**
SvelteKit provides file-based routing, layouts, loading patterns, and a mature
build pipeline out of the box. With `adapter-static` and `ssr: false`, it compiles
to a pure client-side SPA — no server runtime needed. The Go server serves the
static output via `go:embed`. This gives us the routing/layout ergonomics of a
framework with zero runtime cost.

---

## Svelte 5 SPA — Key Views

```
src/
  lib/
    api.ts                   -- fetch wrapper for /api/*
    ws.ts                    -- WebSocket client, reconnect logic
    wasm.ts                  -- load QuickJS WASM, run user JS transforms
    forward.ts               -- fan-out: browser-side fetch to target URLs
    idb.ts                   -- IndexedDB wrapper for browser-mode request history
    stores/
      endpoint.ts            -- current endpoint state
      requests.ts            -- list of captured requests (reactive)
  routes/
    +page.svelte             -- landing: create new endpoint or enter existing
    /[slug]/
      +page.svelte           -- main workspace: request list + detail + config
  components/
    RequestList.svelte       -- sidebar: list of captured requests
    RequestDetail.svelte     -- headers, query, body viewer (formatted JSON/XML/text)
    CodeEditor.svelte        -- Monaco/CodeMirror for JS transform scripts
    ForwardConfig.svelte     -- manage fan-out target URLs
    ResponseViewer.svelte    -- show transform output / forward results
    ModeToggle.svelte        -- switch endpoint between server/browser mode
```

### Browser-side WASM Transform

- Bundle [`quickjs-emscripten`](https://github.com/nicksrandall/aspect-build-quickjs-wasm) (by justjake)
  — a well-maintained TypeScript wrapper around QuickJS compiled to WASM (~400KB gzipped).
- User writes plain JS: `function transform(req) { return { ...req, extra: true }; }`
- SPA calls into QuickJS WASM with the request JSON, gets transformed JSON back.
- Sandboxed execution: user scripts run in the WASM VM, not via `eval()`. No access
  to DOM, network, or host globals. Safe by default.
- Zero server load. Works offline once the page is loaded.
- Also supports Lua (via wasmoon) and Jsonnet (via tplfa-jsonnet) — all browser-side only.

### Browser-side Forwarding

```ts
// forward.ts
async function forwardRequest(req: CapturedRequest, targets: string[]) {
  const results = await Promise.allSettled(
    targets.map(url =>
      fetch(url, {
        method: req.method,
        headers: req.headers,
        body: req.body,
      })
    )
  );
  return results; // show success/failure per target in UI
}
```

Because the browser makes these calls, `http://localhost:3000/webhook` works.
The UI shows status per target (green/red + status code + latency).

---

## API Endpoints

| Method | Path                        | Description                          |
|--------|-----------------------------|--------------------------------------|
| ANY    | `/h/:slug`                  | Capture inbound webhook              |
| ANY    | `/h/:slug/*`                | Capture with sub-path                |
| GET    | `/api/endpoints`            | List user's endpoints (auth)         |
| POST   | `/api/endpoints`            | Create new endpoint                  |
| GET    | `/api/endpoints/:id`        | Get endpoint + config                |
| PATCH  | `/api/endpoints/:id`        | Update endpoint config               |
| DELETE | `/api/endpoints/:id`        | Delete endpoint + requests           |
| GET    | `/api/endpoints/:id/requests` | List captured requests (paginated) |
| GET    | `/api/requests/:reqId`      | Get single request detail            |
| DELETE | `/api/requests/:reqId`      | Delete a request                     |
| DELETE | `/api/endpoints/:id/requests` | Clear all requests for endpoint    |
| WS     | `/ws/:slug`                 | WebSocket stream for live requests   |

---

## Auth

*Skipped by design.* No auth, no OAuth, no user ownership. All endpoints are
anonymous and publicly accessible. The `users` table exists in the schema but is
not used. If auth is needed later, Google OAuth 2.0 is the planned approach.

---

## Deployment

**Minimal (single machine):**
```
docker compose up   # Go binary + Postgres (+ optional Redis)
```

**Binary:**
```
testhooks --db postgres://... --listen :8080
```

SPA is embedded in the Go binary via `go:embed`. One binary, one database. That's it.

**Resource estimate for light usage (< 1000 webhooks/day):**
- Go binary: ~20 MB RAM
- PostgreSQL: ~50 MB RAM
- Total: fits a $5/mo VPS easily

---

## Build & Dev

```bash
# Dev (hot reload both)
cd web && npm run dev          # Svelte dev server on :5173
cd .. && go run ./cmd/testhooks --dev  # Go on :8080, proxies /api to itself, SPA to :5173

# Prod build
cd web && npm run build        # outputs to web/dist/
cd .. && go build -o testhooks ./cmd/testhooks   # embeds web/dist via go:embed
```

---

## Phases

### Phase 1 — MVP
- [x] Go server: capture endpoint, store in Postgres, serve via REST
- [x] WebSocket hub: stream new requests to connected browsers
- [x] Svelte 5 SPA: create endpoint, view request list, inspect detail
- [x] Browser-side forwarding to target URLs (including localhost)
- [x] Server-side WASM transforms via Wazero (runs in capture pipeline)
  - *Note: Uses `github.com/fastschema/qjs` (QuickJS pool) instead of raw Wazero. Server-side transforms support JavaScript only.*
- [x] Server-side forwarding (Go HTTP to public URLs, with retry)
- [x] Docker Compose (Go + Postgres)
- [x] Browser-mode endpoints (thin relay, no DB storage, WS pass-through)
- [x] Bidirectional WebSocket protocol for browser-mode custom responses (server holds request open, browser runs handler, sends result back)
- [x] Background cleanup pruner (age-based + count-based request pruning)
- [x] Per-IP token-bucket rate limiting middleware
- [x] Dark mode (mode-watcher)
- [x] Configurable PORT env var (PaaS convention)

### Phase 2 — Transforms & Polish
- [x] Browser-side QuickJS WASM: user-defined JS transforms (interactive/ad-hoc)
- [x] Code editor in SPA (CodeMirror 6)
- [x] Request search/filter in SPA
- [x] Copy as cURL / replay from UI
- [x] Error handling polish across all frontend views (error banners, optimistic rollback, retry buttons)

### Phase 3 — Auth & Multi-user
*Skipped by design — no auth, no OAuth, no user ownership.*
- [ ] ~~Google OAuth 2.0~~
- [ ] ~~Owned vs anonymous endpoints (TTL differences)~~
- [ ] ~~Endpoint sharing (read-only link)~~

### Phase 4 — Extras
- [ ] Redis pub/sub for horizontal scaling
- [x] Custom response (script-based: handler function receives request, returns response object) per endpoint
  - *Supports JS, Lua, and Jsonnet. Server-side custom responses use JS (QuickJS via fastschema/qjs). Browser-mode uses "server holds request open" pattern with WS round-trip.*
- [x] Rate limiting (per-IP token bucket)
- [x] Export (JSON, CSV)
- [x] Additional WASM languages: Lua (wasmoon), Jsonnet (tplfa-jsonnet) — browser-side only
  - *Note: Go via TinyGo and Rust not implemented. Server-side only supports JS.*

---

## Known Issues & Fixes

### Fix: Saving transform config overwrote forward URL (and vice versa)

**Root cause:** `web/src/routes/[slug]/+page.svelte` held a local `endpoint`
variable assigned once in `init()`. After saving settings from one tab (e.g.
forwarding), the local variable was never refreshed. When a different tab (e.g.
transforms) later spread `endpoint.config` into its PATCH payload, it sent the
**stale** config — missing keys added by the other tab. The backend PATCH handler
(`internal/handler/api.go`) does a full replacement of the `config` JSONB column,
so missing keys were deleted.

**Fix:** `saveTransformScript()` now reads from `endpointStore.current` (the
reactive store that is updated after every successful PATCH) instead of the stale
local variable.

**Design note:** The backend intentionally replaces the entire `config` JSONB on
PATCH rather than deep-merging. This is simpler and avoids ambiguity about key
deletion. The frontend is responsible for sending the complete config. If more
independent config sections are added in the future, consider either (a) splitting
config into separate columns/endpoints, or (b) adding server-side JSON merge logic.

### Fix: Incorrect forwarding description in UI

**Root cause:** `ForwardConfig.svelte` tooltip and info box text stated the forward
target's response is "returned directly to the webhook sender." This was inaccurate
— in sync mode, the forward response is passed through the custom response handler
pipeline (if configured) before the final HTTP response is built.

**Fix:** Updated tooltip and info box text to accurately describe the pipeline:
the forward target's response is available to the custom response handler, which
can use it to build the final response. Without a handler, the target's response
is sent back as-is.
