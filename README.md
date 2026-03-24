<p align="center">
  <a href="https://testhooks.sarathsadasivan.com/">
    <img src=".github/logo.svg" alt="Testhooks" width="700" />
  </a>
</p>

<p align="center">
  <strong>A lightweight, self-hostable <a href="https://webhook.site">webhook.site</a> replacement.</strong><br/>
  Ships as a single binary with the SPA embedded — just point it at Postgres and go.
</p>

<p align="center">
  <a href="#quick-start">Quick Start</a> &middot;
  <a href="#features">Features</a> &middot;
  <a href="#how-it-works">How It Works</a> &middot;
  <a href="#api">API</a> &middot;
  <a href="#configuration">Configuration</a> &middot;
  <a href="https://testhooks.sarathsadasivan.com/">Live Demo</a>
</p>

---

## Why Testhooks?

- **No signup, no SaaS** — run it on your own infra in under a minute.
- **Replace ngrok for webhook testing** — webhooks hit the public server, stream to your browser via WebSocket, and forward to `localhost` via `fetch()`. No CLI, no daemon, no port forwarding.
- **Privacy when you need it** — Browser mode means payloads never touch the server's disk. All processing happens client-side.
- **Always-on when you need it** — Server mode stores everything in Postgres and runs transforms/forwards headlessly, even when no browser is open.
- **Single binary, one dependency** — Go binary with the SPA embedded via `go:embed`. PostgreSQL is the only external requirement.

---

## Features

|  |  |
|---|---|
| **Unique webhook URLs** | Each endpoint gets a short URL (`/h/a1b2c3d4`) |
| **Real-time streaming** | Requests pushed to the browser instantly via WebSocket |
| **Two endpoint modes** | Server mode (persistent, always-on) or Browser mode (zero storage, privacy-first) |
| **Browser-side forwarding** | Forward to `localhost` or any URL via `fetch()` |
| **Server-side forwarding** | Reliable forwarding from Go to public URLs |
| **WASM transforms** | JavaScript on the server (QuickJS), JS + Lua + Jsonnet in the browser |
| **Custom responses** | Script-based HTTP response control per endpoint |
| **Code editor** | CodeMirror 6 built in for writing transform scripts |
| **Export** | Download captured requests as JSON or CSV |
| **Copy as cURL** | Replay requests from the UI |
| **Dark mode** | System / light / dark theme toggle |
| **Rate limiting** | Per-IP token-bucket middleware |

---

## How It Works

```
                                    ┌──────────────────────────────────────────┐
                                    │            Browser (Svelte 5)            │
                                    │                                          │
                                    │  inspect ─► transform (WASM) ─► forward  │
                                    │                                 fetch()  │
                                    └──────────┬───────────────────┬───────────┘
                                               │ WebSocket         │
                                               │                   ▼
  webhook sender ──► POST /h/:slug ──► Go Server ──► Postgres    localhost
                                          │
                                          ▼ (optional)
                                       Redis Pub/Sub
```

**Server mode** — The Go server stores every request in Postgres, runs WASM transforms via QuickJS, and forwards to configured URLs. The browser is just a viewer. Works headlessly, always on.

**Browser mode** — The server acts as a thin relay: receives the webhook, streams it over WebSocket, and discards it immediately. All transforms run in-browser via WASM. Forward to `localhost` with `fetch()`. Nothing written to disk.

---

## Quick Start

### Docker Compose (easiest)

```bash
git clone https://github.com/yourusername/testhooks.git
cd testhooks
docker compose up --build
```

Open **http://localhost:8080** and start sending webhooks.

### Docker image + existing Postgres

```bash
docker build -t testhooks .
docker run -p 8080:8080 \
  -e DATABASE_URL="postgres://user:pass@your-db-host:5432/testhooks?sslmode=disable" \
  testhooks
```

### Pre-built binary

```bash
make build
DATABASE_URL="postgres://user:pass@localhost:5432/testhooks?sslmode=disable" ./testhooks
```

The binary includes the SPA — there are no static files to serve separately. Cross-compile for all platforms with `make build-all`.

### From source

**Prerequisites:** Go 1.22+, Node 20+, PostgreSQL 15+

```bash
cd web && npm ci && cd ..
make build
./testhooks
```

---

## Development

```bash
# Start both Go backend and Vite dev server with hot reload
make dev
```

This starts **Go** on `:8080` (API + WebSocket + reverse proxy to Vite) and **Vite** on `:5173` (SPA with HMR). Open http://localhost:8080 for the full experience.

```bash
make dev-api   # Go backend only
make dev-web   # Vite dev server only
```

---

## Configuration

All configuration is via environment variables. Copy `.env.example` to `.env` to get started.

| Variable | Default | Description |
|----------|---------|-------------|
| `LISTEN` | `:8080` | Address the HTTP server binds to |
| `PORT` | *unset* | Overrides `LISTEN` with `:<PORT>` (PaaS convention) |
| `DATABASE_URL` | `postgres://testhooks:testhooks@localhost:5432/testhooks?sslmode=disable` | PostgreSQL connection string. Migrations run automatically |
| `DEV` | `false` | Proxy SPA requests to the Vite dev server |
| `VITE_URL` | `http://localhost:5173` | Vite dev server URL (only when `DEV=true`) |
| `MAX_BODY_SIZE` | `524288` (512 KB) | Max request body size for captured webhooks |
| `MAX_ENDPOINT_STORAGE_BYTES` | `10485760` (10 MB) | Total body storage budget per endpoint |
| `MAX_REQUESTS_PER_ENDPOINT` | `500` | Max stored requests per endpoint |
| `PRUNE_INTERVAL_SECONDS` | `60` | How often the background pruner runs |
| `RING_BUFFER_SIZE` | `100` | Per-endpoint in-memory ring buffer for browser-mode |
| `RATE_LIMIT_RPS` | `20` | Sustained requests/sec per IP on capture endpoints (`0` to disable) |
| `RATE_LIMIT_BURST` | `40` | Max burst size per IP |

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

| Layer | Technology | Notes |
|-------|-----------|-------|
| Backend | **Go** | stdlib `net/http`, single binary, `go:embed` for SPA |
| Frontend | **Svelte 5** | SvelteKit SPA mode, `adapter-static` |
| Database | **PostgreSQL** | JSONB for request payloads |
| Real-time | **WebSockets** | `nhooyr.io/websocket` |
| WASM (server) | **QuickJS** | `fastschema/qjs` pool for server-side JS transforms |
| WASM (browser) | **QuickJS + Lua + Jsonnet** | `quickjs-emscripten`, `wasmoon`, `tplfa-jsonnet` |

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
