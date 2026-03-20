# Testhooks

A lightweight, self-hostable [webhook.site](https://webhook.site) replacement.

Capture, inspect, transform, and forward webhooks in real time.
Ships as a **single binary** with the SPA embedded — just point it at a PostgreSQL database and go.

```
webhook sender ──► https://hooks.example.com/h/a1b2c3d4 ──► your browser (live)
```

---

## Why Testhooks?

- **No signup, no SaaS** — run it on your own infra in under a minute.
- **Replace ngrok for webhook testing** — webhooks hit the public server, stream to your browser via WebSocket, and forward to `localhost` via `fetch()`. No CLI, no daemon, no port forwarding.
- **Privacy when you need it** — Browser mode means payloads never touch the server's disk. All processing happens client-side.
- **Always-on when you need it** — Server mode stores everything in Postgres and runs transforms/forwards headlessly, even when no browser is open.
- **Single binary, one dependency** — Go binary with the SPA embedded via `go:embed`. PostgreSQL is the only external requirement.

---

## Features

- **Unique webhook URLs** — each endpoint gets a short URL (`/h/a1b2c3d4`)
- **Real-time streaming** — requests pushed to the browser instantly via WebSocket
- **Two endpoint modes** — Server mode (persistent, always-on) or Browser mode (zero storage, privacy-first)
- **Browser-side forwarding** — forward to `localhost` or any URL via `fetch()`
- **Server-side forwarding** — reliable forwarding from Go to public URLs
- **WASM transforms** — JavaScript on the server (QuickJS), JS + Lua + Jsonnet in the browser
- **Custom responses** — script-based HTTP response control per endpoint
- **Code editor** — CodeMirror 6 built in for writing transform scripts
- **Export** — download captured requests as JSON or CSV
- **Copy as cURL** — replay requests from the UI
- **Dark mode** — system / light / dark theme toggle
- **Rate limiting** — per-IP token-bucket middleware

---

## Quick Start

### Option 1: Docker Compose (easiest)

The fastest way to get running. This starts the app and a PostgreSQL instance together.

```bash
git clone https://github.com/yourusername/testhooks.git
cd testhooks
docker compose up --build
```

Open **http://localhost:8080** and start sending webhooks.

### Option 2: Docker image + existing Postgres

If you already have a PostgreSQL database, run just the app container:

```bash
docker build -t testhooks .
docker run -p 8080:8080 \
  -e DATABASE_URL="postgres://user:pass@your-db-host:5432/testhooks?sslmode=disable" \
  testhooks
```

### Option 3: Pre-built binary

Download (or build) the binary and run it directly. No containers needed.

```bash
# Build for your platform
make build

# Or cross-compile for all platforms
make build-all    # outputs to dist/

# Run it
DATABASE_URL="postgres://user:pass@localhost:5432/testhooks?sslmode=disable" ./testhooks
```

The binary includes the SPA — there are no static files to serve separately.

### Option 4: From source (development)

**Prerequisites:** Go 1.22+, Node 20+, PostgreSQL 15+

```bash
# Install frontend dependencies and build the SPA
cd web && npm ci && cd ..

# Full production build (SPA + Go binary)
make build

# Run
./testhooks
```

---

## Development

```bash
# Start both Go backend and Vite dev server with hot reload
make dev
```

This starts:
- **Go** on http://localhost:8080 (API + WebSocket + reverse proxy to Vite)
- **Vite** on http://localhost:5173 (SPA with hot module replacement)

Open http://localhost:8080 for the full experience. The Go server proxies non-API requests to Vite in dev mode.

You can also run them separately:

```bash
make dev-api   # Go backend only (proxies / to Vite on :5173)
make dev-web   # Vite dev server only
```

---

## Configuration

All configuration is via environment variables. Copy `.env.example` to `.env` to get started.

**`LISTEN`** (default: `:8080`)
The address the HTTP server binds to.

**`PORT`** (default: *unset*)
When set, overrides `LISTEN` with `:<PORT>`. This follows the standard PaaS convention used by Heroku, Railway, Render, etc.

**`DATABASE_URL`** (default: `postgres://testhooks:testhooks@localhost:5432/testhooks?sslmode=disable`)
PostgreSQL connection string. The server runs migrations automatically on startup.

**`DEV`** (default: `false`)
Enable development mode. When true, the Go server proxies SPA requests to the Vite dev server instead of serving the embedded static files.

**`VITE_URL`** (default: `http://localhost:5173`)
URL of the Vite dev server. Only used when `DEV=true`.

**`MAX_BODY_SIZE`** (default: `524288` / 512 KB)
Maximum request body size in bytes for captured webhooks. Larger payloads are truncated.

**`MAX_ENDPOINT_STORAGE_BYTES`** (default: `10485760` / 10 MB)
Total body storage budget per endpoint. When exceeded, the oldest requests are pruned automatically by the background cleaner.

**`MAX_REQUESTS_PER_ENDPOINT`** (default: `500`)
Maximum number of stored requests per endpoint. Oldest are pruned when this limit is exceeded.

**`PRUNE_INTERVAL_SECONDS`** (default: `60`)
How often the background pruner runs to enforce storage budgets.

**`RING_BUFFER_SIZE`** (default: `100`)
Per-endpoint in-memory ring buffer for browser-mode endpoints. Covers brief disconnects (page reloads, network blips) without writing anything to disk.

**`RATE_LIMIT_RPS`** (default: `20`)
Sustained requests per second allowed per IP on capture endpoints. Set to `0` to disable.

**`RATE_LIMIT_BURST`** (default: `40`)
Maximum burst size per IP for rate limiting.

---

## API

| Method | Path | Description |
|--------|------|-------------|
| `ANY` | `/h/:slug` | Capture inbound webhook |
| `ANY` | `/h/:slug/*` | Capture with sub-path |
| `GET` | `/api/endpoints` | List endpoints |
| `POST` | `/api/endpoints` | Create endpoint |
| `GET` | `/api/endpoints/:id` | Get endpoint |
| `PATCH` | `/api/endpoints/:id` | Update endpoint config |
| `DELETE` | `/api/endpoints/:id` | Delete endpoint + requests |
| `GET` | `/api/endpoints/:id/requests` | List requests (paginated) |
| `DELETE` | `/api/endpoints/:id/requests` | Clear all requests |
| `GET` | `/api/requests/:reqId` | Get single request |
| `DELETE` | `/api/requests/:reqId` | Delete a request |
| `WS` | `/ws/:slug` | WebSocket stream |

---

## Tech Stack

- **Backend** — Go (stdlib `net/http`, single binary)
- **Frontend** — Svelte 5 (SvelteKit SPA mode, `adapter-static`)
- **Database** — PostgreSQL (JSONB for request payloads)
- **Real-time** — WebSockets (`nhooyr.io/websocket`)
- **WASM (server)** — QuickJS (`fastschema/qjs` pool)
- **WASM (browser)** — QuickJS + Lua (wasmoon) + Jsonnet (tplfa-jsonnet)

---

## Makefile Targets

```
make help         Show all targets
make dev          Run Go + Vite concurrently (hot reload)
make dev-api      Run Go backend only
make dev-web      Run Vite dev server only
make build        Full production build (SPA + Go binary)
make build-all    Cross-compile for all OS/arch combinations
make build-web    Build SPA into web/dist/
make build-go     Build Go binary (assumes web/dist/ exists)
make run          Run the compiled binary
make docker-build Build the Docker image
make docker-up    Start app + Postgres via Docker Compose
make docker-down  Stop containers and remove volumes
make lint         Run Go vet + Svelte check
make test         Run Go tests
make clean        Remove build artifacts
```

---

## License

MIT
