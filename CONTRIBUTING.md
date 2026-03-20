# Contributing to Testhooks

## Architecture Overview

Testhooks is a single-binary Go application that serves a Svelte 5 SPA. The SPA is built to static files, embedded into the Go binary via `go:embed`, and served alongside the API and WebSocket endpoints.

```
webhook sender ──► Go Server ──► PostgreSQL
                       │
                       ├── REST API  (/api/*)
                       ├── Capture   (/h/:slug)
                       └── WebSocket (/ws/:slug) ──► Browser (Svelte 5 SPA)
```

### Request Flow

**Server-mode endpoints:**
1. External request hits `POST /h/:slug`
2. Go handler reads method, headers, query, body
3. Request is inserted into the `requests` table in Postgres
4. Request is published to the in-memory hub
5. All connected WebSocket clients for that slug receive the request in real time

**Browser-mode endpoints:**
1. External request hits `POST /h/:slug`
2. Go handler reads the request but does **not** write to Postgres
3. Request is published directly to the WebSocket hub
4. If no browser is connected, the server buffers briefly in a ring buffer, then drops
5. The browser handles all transforms and forwarding via `fetch()`

## Project Structure

```
cmd/
  testhooks/main.go              Entry point, router, graceful shutdown

internal/
  config/config.go               Environment variable config (caarlos0/env)
  db/
    db.go                        PostgreSQL pool + auto-migration
    queries.go                   All SQL queries (pgx, no ORM)
    migrations/
      001_init.up.sql            Schema: endpoints + requests tables
      001_init.down.sql          Drop migration
  handler/
    capture.go                   POST/PUT/GET/etc /h/:slug — webhook capture
    api.go                       REST API: CRUD endpoints + requests
    ws.go                        WebSocket upgrade + read/write pumps
  hub/
    hub.go                       In-memory pub/sub with ring buffer
  cleanup/
    pruner.go                    Background goroutine for TTL/count pruning

web/                             Svelte 5 SPA
  embed.go                       go:embed for web/dist/*
  src/
    lib/
      types.ts                   TypeScript interfaces
      api.ts                     REST API fetch wrapper
      ws.ts                      WebSocket client with auto-reconnect
      forward.ts                 Browser-side fan-out forwarding
      utils.ts                   Formatting helpers, clipboard, etc.
      stores/
        endpoint.svelte.ts       Svelte 5 rune store for current endpoint
        requests.svelte.ts       Svelte 5 rune store for request list
      components/
        RequestList.svelte       Sidebar: captured request list
        RequestDetail.svelte     Full request inspector
        ForwardConfig.svelte     Settings: name, mode, forward URLs
    routes/
      +layout.ts                 SSR disabled, SPA mode
      +layout.svelte             Root layout with header
      +page.svelte               Landing: create endpoint, list endpoints
      [slug]/
        +page.svelte             Workspace: live request stream + inspector
  dist/                          Built SPA output (embedded into Go binary)
```

## Key Design Decisions

### No HTTP framework
The Go server uses `net/http` with Go 1.22+ route patterns (`GET /api/endpoints/{id}`). No router library. The `PathValue()` method extracts path parameters.

### No ORM
All database queries are written directly with `pgx/v5`. The `db/queries.go` file contains every query as a method on the `Pool` struct. This keeps things explicit and avoids magic.

### Hub pattern
`hub/hub.go` manages per-slug subscriber lists using goroutine + channels. When a webhook arrives, the capture handler publishes to the hub, which fans out to all connected WebSocket clients. For browser-mode endpoints, a ring buffer holds recent requests so reconnecting browsers get catch-up data.

### SPA embedding
The SvelteKit SPA builds to `web/dist/` via `adapter-static`. The Go binary embeds this directory using `go:embed` in `web/embed.go`. At runtime, `main.go` serves the embedded filesystem for any path that doesn't match `/api/*`, `/h/*`, or `/ws/*`, with a fallback to `index.html` for SPA client-side routing.

### Svelte 5 runes
Stores use `.svelte.ts` files with `$state` and `$derived` runes instead of Svelte 4's writable/readable stores. This is the Svelte 5 way.

## Development Setup

### Prerequisites

- **Go 1.22+** — `go version`
- **Node 20+** — `node --version`
- **PostgreSQL 15+** — local or via Docker

### First time setup

```bash
# Install frontend dependencies
cd web && npm ci && cd ..

# Start Postgres (easiest via Docker)
docker compose up -d postgres

# Start dev servers (Go + Vite hot reload)
make dev
```

The Go server runs on `:8080` and proxies SPA requests to Vite on `:5173` when `DEV=true`.

### Making changes

**Backend (Go):**
- Edit files in `internal/` or `cmd/`
- Restart the Go server (`make dev-api` or `Ctrl+C` and re-run `make dev`)
- Use `go vet ./...` and `go test ./...` before committing

**Frontend (Svelte):**
- Edit files in `web/src/`
- Vite hot-reloads automatically
- Use `cd web && npm run check` for type checking

**Database migrations:**
- Add new `.sql` files in `internal/db/migrations/` with incrementing prefixes
- Migrations run automatically on startup via `golang-migrate`

### Production build

```bash
make build
```

This runs `npm ci && npm run build` in `web/`, then `go build` with `-trimpath -ldflags="-s -w"`. The output is a single static binary.

### Docker

```bash
# Build and start everything
docker compose up --build

# Just build the image
make docker-build

# Stop and clean up
make docker-down
```

The Dockerfile uses a multi-stage build:
1. **Node stage** — builds the SPA
2. **Go stage** — compiles the binary with embedded SPA
3. **Scratch stage** — final image with just the binary, CA certs, and tzdata

## Database Schema

Two main tables:

- **`endpoints`** — webhook URL configurations (slug, name, mode, config JSONB)
- **`requests`** — captured inbound webhooks (method, headers, body, metadata)

The `config` JSONB column on `endpoints` stores forward URLs and other settings:
```json
{
  "forward_urls": ["https://example.com/webhook", "http://localhost:3000/hook"]
}
```

## WebSocket Protocol

Clients connect to `WS /ws/:slug`. Messages are JSON-encoded `CapturedRequest` objects pushed from the server when a new webhook arrives. The client maintains the connection with ping/pong keep-alive.

## Adding a New API Endpoint

1. Add the SQL query method in `internal/db/queries.go`
2. Add the HTTP handler in `internal/handler/api.go`
3. Register the route in `cmd/testhooks/main.go`
4. Add the TypeScript API call in `web/src/lib/api.ts`
5. Wire it into the Svelte component

## Code Style

- **Go:** `gofmt` + `go vet`. Standard library conventions. No global state — everything is passed via handler structs or function arguments.
- **TypeScript/Svelte:** Prettier + ESLint. Tailwind CSS for styling. Svelte 5 runes for reactivity.
- **SQL:** Lowercase keywords, descriptive column names, always use parameterized queries (`$1`, `$2`, etc.).
