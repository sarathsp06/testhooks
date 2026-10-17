<p align="center">
  <a href="https://testhooks.sarathsadasivan.com/">
    <img src=".github/logo.svg" alt="Testhooks" width="700" />
  </a>
</p>

<p align="center">
  <strong>A lightweight, self-hostable <a href="https://webhook.site">webhook.site</a> replacement.</strong><br/>
  Capture, inspect, transform, forward and mock webhooks. One binary with the UI embedded, plus PostgreSQL.
</p>

<p align="center">
  <a href="#quick-start">Quick start</a> &middot;
  <a href="#features">Features</a> &middot;
  <a href="#how-it-works">How it works</a> &middot;
  <a href="#verifying-signed-webhooks">Signed webhooks</a> &middot;
  <a href="#configuration">Configuration</a> &middot;
  <a href="https://sarathsp06.github.io/testhooks/">Docs</a> &middot;
  <a href="https://testhooks.sarathsadasivan.com/">Live demo</a>
</p>

Full documentation: **https://sarathsp06.github.io/testhooks/**

---

## Quick start

### Docker Compose (one line)

Runs testhooks and PostgreSQL from the published image. No clone, no build:

```bash
curl -fsSL https://raw.githubusercontent.com/sarathsp06/testhooks/main/deploy/docker-compose.yml | docker compose -p testhooks -f - up -d
```

Open **http://localhost:8080**, create an endpoint, and send it a request:

```bash
curl -X POST http://localhost:8080/h/<slug> -H 'Content-Type: application/json' -d '{"hello":"world"}'
```

Set options inline, for example `TESTHOOKS_PORT=9000 AUTH_TOKEN=change-me curl -fsSL … | docker compose -p testhooks -f - up -d`. Stop it with `docker compose -p testhooks down` (add `-v` to delete the data too).

### Install the binary (one line)

Downloads the right build for your OS and architecture from GitHub Releases and checks its SHA-256:

```bash
curl -fsSL https://raw.githubusercontent.com/sarathsp06/testhooks/main/install.sh | sh
```

It installs to `/usr/local/bin` when that's writable, otherwise `~/.local/bin`. Use `TESTHOOKS_VERSION=v1.1.0` to pin a release and `TESTHOOKS_INSTALL_DIR=…` to choose the directory. The binary needs PostgreSQL:

```bash
docker run -d --name testhooks-pg -p 127.0.0.1:5432:5432 \
  -e POSTGRES_USER=testhooks -e POSTGRES_PASSWORD=testhooks -e POSTGRES_DB=testhooks \
  postgres:17-alpine
testhooks          # defaults to postgres://testhooks:testhooks@localhost:5432/testhooks
```

Migrations run automatically on startup, and the server waits up to 60 s for the database to come up.

### From source

Prerequisites: Go 1.26+, Node 22+, PostgreSQL 15+.

```bash
git clone https://github.com/sarathsp06/testhooks.git && cd testhooks
docker compose up --build        # app + Postgres, built from this checkout
# or
make build && ./testhooks        # single binary with the SPA embedded
```

`make build-all` cross-compiles for Linux, macOS and Windows (amd64 and arm64) into `dist/`.

---

## Why testhooks?

- **No signup, no SaaS.** Run it on your own infra in under a minute.
- **Test webhooks against localhost without ngrok.** Webhooks hit the public server, stream to your browser over WebSocket, and your browser forwards them to `localhost` with `fetch()`. No CLI, daemon or tunnel.
- **Private when you need it.** In browser mode, payloads are relayed and never written to the server's disk.
- **Always on when you need it.** In server mode, everything is stored in Postgres, and transforms and forwarding run headlessly with no browser open.
- **Small footprint.** One Go binary with the Svelte UI embedded. PostgreSQL is the only dependency.

---

## Features

| | |
|---|---|
| **Unique webhook URLs** | Every endpoint gets a short URL, `/h/a1b2c3d4`; sub-paths (`/h/a1b2c3d4/any/path`) are captured too |
| **Real-time streaming** | Requests are pushed to the browser instantly over WebSocket |
| **Two endpoint modes** | Server mode (persistent, always on) or browser mode (no server storage) |
| **Forwarding** | Server-side to public URLs (async or sync, with retries and SSRF protection), or browser-side to anything including `localhost` |
| **Transforms** | Reshape requests with JavaScript on the server (QuickJS on WASM), or JS, Lua and Jsonnet in the browser |
| **Custom responses** | A script decides the status, headers and body returned to the sender, optionally using the forward target's response |
| **Signature verification** | Checks [Standard Webhooks](https://www.standardwebhooks.com) signatures (Sparrow, Svix and others), HMAC and Ed25519 |
| **Code editor** | CodeMirror 6 for writing scripts |
| **Export and replay** | Download requests as JSON or CSV, or copy any request as a cURL command |
| **Protection** | Optional bearer-token auth, per-IP rate limiting, body size and storage limits, automatic pruning |
| **Themes** | System, light or dark |

---

## How it works

```
                                     ┌───────────────────────────────────────────┐
                                     │             Browser (Svelte 5)            │
                                     │                                           │
                                     │  inspect ─► transform (WASM) ─► forward   │
                                     │                                  fetch()  │
                                     └───────────▲──────────────────────┬────────┘
                                                 │ WebSocket            ▼
  webhook sender ──► POST /h/:slug ──► Go server ┤                  localhost
                                         │       └─► transform ─► forward (server mode)
                                         ▼
                                     PostgreSQL
```

**Server mode** stores every request in Postgres, runs the transform on the server, forwards to the configured URL and returns the custom response. The browser is only a viewer, so it keeps working with no tab open.

**Browser mode** makes the server a thin relay: it receives the webhook, streams it over WebSocket and keeps only a short in-memory buffer so a reconnecting tab can catch up. Transforms and forwarding run in your browser, which is how it reaches `localhost`. Turn on *Persist requests* to keep a server-side copy as well.

The server is required in both modes because something public has to receive the webhook. A static build of the UI alone can't accept incoming HTTP requests.

### Responses webhook senders see

| Status | When |
|---|---|
| `200` | Captured (server mode), or whatever your custom response or sync forward target returned |
| `202` | Captured and relayed (browser mode) |
| `401` | Signature verification is enforced and the signature is missing or invalid |
| `404` | No endpoint with that slug |
| `413` | Body larger than `MAX_BODY_SIZE` |
| `429` | Rate limited (`Retry-After: 1`) |
| `503` | Database unavailable. Senders that retry on 5xx will redeliver |

---

## Verifying signed webhooks

testhooks can verify [Standard Webhooks](https://www.standardwebhooks.com) signatures, the `webhook-id`, `webhook-timestamp` and `webhook-signature` headers sent by [Sparrow](https://sarathsp06.github.io/sparrow/), Svix and others.

In an endpoint's **Settings → Signature Verification**, paste the sender's secret (`whsec_…`, or any raw secret) and/or its Ed25519 public key (hex). Each captured request then shows **verified** or **invalid**, with the reason and the `webhook-id`, which stays the same across retries so duplicates are easy to spot.

Turn on **Reject invalid signatures with 401** to make the endpoint behave like a real receiver. Rejected requests are still captured so you can inspect them. The same settings are available through the API. `PATCH` replaces the whole `config`, so include any other settings you want to keep:

```bash
curl -X PATCH http://localhost:8080/api/endpoints/<id> -H 'Content-Type: application/json' \
  -d '{"config":{"signature":{"secret":"whsec_...","enforce":true}}}'
```

Timestamps more than 5 minutes off are rejected as replays. The verifier is tested against Sparrow's published signature test vectors.

---

## Configuration

Everything is configured with environment variables. A `.env` file in the working directory is loaded if present; see [`.env.example`](.env.example).

| Variable | Default | Description |
|---|---|---|
| `DATABASE_URL` | `postgres://testhooks:testhooks@localhost:5432/testhooks?sslmode=disable` | PostgreSQL connection string |
| `LISTEN` | `:8080` | Bind address |
| `PORT` | | Overrides `LISTEN` with `:<PORT>` (PaaS convention) |
| `AUTH_TOKEN` | | When set, the API and WebSocket require this token and the UI prompts for it. `/h/*` and `/healthz` stay public |
| `ALLOWED_ORIGINS` | `*` | Comma-separated CORS and WebSocket origins. Set this in production |
| `ALLOWED_HEADERS` | `Content-Type,Authorization,X-Requested-With` | CORS allowed headers |
| `TRUSTED_PROXIES` | | Comma-separated CIDRs allowed to set `X-Forwarded-For` / `X-Real-IP`, such as your load balancer |
| `MAX_BODY_SIZE` | `524288` | Max captured body in bytes (512 KB) |
| `MAX_ENDPOINT_STORAGE_BYTES` | `10485760` | Per-endpoint storage budget; oldest requests are pruned beyond it |
| `MAX_REQUESTS_PER_ENDPOINT` | `500` | Per-endpoint request cap; oldest are pruned beyond it |
| `PRUNE_INTERVAL_SECONDS` | `60` | How often the pruner runs |
| `RATE_LIMIT_RPS` | `20` | Per-IP requests/second on capture and API routes (`0` disables) |
| `RATE_LIMIT_BURST` | `40` | Per-IP burst size |
| `API_TIMEOUT_SECONDS` | `30` | Timeout for REST API requests |
| `RING_BUFFER_SIZE` | `100` | Browser-mode catch-up buffer per endpoint (in memory) |
| `RING_BUFFER_TTL_SECONDS` | `300` | How long buffered browser-mode requests are kept |
| `MAX_SUBSCRIBERS_PER_SLUG` | `50` | Max concurrent WebSocket viewers per endpoint |
| `DEV` | `false` | Proxy the UI to the Vite dev server |
| `VITE_URL` | `http://localhost:5173` | Vite dev server URL when `DEV=true` |

### Running in production

- Set `AUTH_TOKEN` and `ALLOWED_ORIGINS`. Without them anyone who can reach the server can read every endpoint.
- Put it behind a TLS-terminating reverse proxy and list that proxy in `TRUSTED_PROXIES` so rate limiting sees real client IPs.
- `GET /healthz` returns `200` when the database is reachable and `503` when it isn't. Use it for liveness and readiness checks.
- On `SIGTERM` the server stops accepting requests and gives in-flight requests and background forwards up to 10 s to finish.

---

## Development

```bash
make dev        # Go on :8080 (API + WebSocket + proxy to Vite) and Vite on :5173 with hot reload
make dev-api    # Go backend only
make dev-web    # Vite dev server only
make test       # Go tests (the web package's tests need `make build-web` first)
make lint       # go vet + svelte-check
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for more. The documentation site lives in [`docs/`](docs/) (`cd docs && npm ci && npm run dev`) and deploys to GitHub Pages from `main`.

### Releasing

Push a tag such as `v1.1.0`. The [release workflow](.github/workflows/release.yml) attaches binaries and checksums to the GitHub release and publishes `ghcr.io/sarathsp06/testhooks:1.1.0` and `:latest` for amd64 and arm64. The one-line installers above download from these.

---

## License

[Apache License 2.0](LICENSE)
