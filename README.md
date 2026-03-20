# Testhooks

A lightweight, self-hostable [webhook.site](https://webhook.site) replacement.

Capture, inspect, transform, and forward webhooks in real time.
Ships as a **single binary** — the Go backend serves the SPA, API, and WebSocket connections. PostgreSQL is the only required dependency.

## Features

- **Unique webhook URLs** — each endpoint gets a short URL (e.g. `/h/a1b2c3d4`)
- **Real-time streaming** — inbound requests are pushed to the browser via WebSocket
- **Two endpoint modes:**
  - **Server mode** — requests stored in Postgres, always-on, headless transforms and forwarding
  - **Browser mode** — zero storage, privacy-first; all processing happens in the browser
- **Replace ngrok for webhook testing** — Browser mode acts as an HTTP tunnel: webhooks hit the public server, stream to your browser via WebSocket, and forward to `localhost` via `fetch()`. No CLI, no daemon, no port forwarding
- **Browser-side forwarding** — forward webhooks to `localhost` or any URL via `fetch()`
- **Server-side forwarding** — reliable forwarding from the Go server to public URLs (sync or async)
- **WASM transforms** — run JavaScript transforms server-side (QuickJS via fastschema/qjs) or browser-side (QuickJS WASM). Browser also supports Lua (wasmoon) and Jsonnet (tplfa-jsonnet)
- **Custom responses** — script-based HTTP response control (JS/Lua/Jsonnet); server-side uses QuickJS, browser-mode uses bidirectional WebSocket round-trip
- **Export** — download captured requests as JSON or CSV
- **Code editor** — CodeMirror 6 for writing transform and handler scripts
- **Dark mode** — system/light/dark theme toggle via mode-watcher
- **Rate limiting** — per-IP token-bucket middleware
- **Single binary deployment** — SPA embedded via `go:embed`, no runtime file dependencies

## Quick Start

### Docker Compose (recommended)

```bash
docker compose up --build
```

Open http://localhost:8080 — create an endpoint and start sending webhooks.

### From Source

**Prerequisites:** Go 1.22+, Node 20+, PostgreSQL 15+

```bash
# 1. Install dependencies
cd web && npm ci && cd ..

# 2. Full production build (SPA + Go binary)
make build

# 3. Start Postgres (if not already running)
docker compose up -d postgres

# 4. Run
./testhooks
```

### Development

```bash
# Start both Go backend and Vite dev server with hot reload
make dev
```

This starts:
- **Go** on http://localhost:8080 (API + WebSocket + reverse proxy to Vite)
- **Vite** on http://localhost:5173 (SPA with hot module replacement)

In dev mode, the Go server proxies non-API requests to Vite. Open http://localhost:8080 for the full experience.

You can also run them separately:

```bash
make dev-api   # Go backend only (proxies / to Vite on :5173)
make dev-web   # Vite dev server only
```

## Configuration

All configuration is via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `LISTEN` | `:8080` | HTTP listen address |
| `PORT` | *(unset)* | Overrides `LISTEN` when set (standard PaaS convention: `:<PORT>`) |
| `DATABASE_URL` | `postgres://testhooks:testhooks@localhost:5432/testhooks?sslmode=disable` | PostgreSQL connection string |
| `DEV` | `false` | Enable dev mode (proxy SPA to Vite) |
| `VITE_URL` | `http://localhost:5173` | Vite dev server URL (dev mode only) |
| `MAX_BODY_SIZE` | `524288` | Max request body size in bytes (512KB) |
| `MAX_ENDPOINT_STORAGE_BYTES` | `10485760` | Max body storage per endpoint (10MB); oldest pruned when exceeded |
| `MAX_REQUESTS_PER_ENDPOINT` | `500` | Max stored requests per endpoint |
| `PRUNE_INTERVAL_SECONDS` | `60` | How often the background pruner runs |
| `RING_BUFFER_SIZE` | `100` | Per-endpoint ring buffer for browser-mode catch-up |

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
| `WS` | `/ws/:slug` | WebSocket stream for live requests |

## Makefile Targets

```
make help         Show all targets
make dev          Run Go + Vite concurrently (hot reload)
make dev-api      Run Go backend only
make dev-web      Run Vite dev server only
make build        Full production build (SPA + Go binary)
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

## Tech Stack

| Layer | Choice |
|-------|--------|
| Backend | Go (stdlib `net/http`, single binary) |
| Frontend | Svelte 5 (SvelteKit SPA mode, `adapter-static`) |
| Database | PostgreSQL (JSONB for request payloads) |
| Real-time | WebSockets (`nhooyr.io/websocket`) |
| WASM (server) | QuickJS (`fastschema/qjs` pool, runs in capture pipeline) |
| WASM (browser) | QuickJS + Lua (wasmoon) + Jsonnet (tplfa-jsonnet) |

## License

MIT
