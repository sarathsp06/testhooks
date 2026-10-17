<p align="center">
  <a href="https://testhooks.vercel.app/">
    <img src=".github/logo.svg" alt="Testhooks" width="700" />
  </a>
</p>

<p align="center">
  <strong>A lightweight, self-hostable <a href="https://webhook.site">webhook.site</a> replacement.</strong><br/>
  Capture, inspect, transform, forward and mock webhooks. One binary with the UI embedded; PostgreSQL or in-memory storage.
</p>

<p align="center">
  <a href="#quick-start">Quick start</a> &middot;
  <a href="#features">Features</a> &middot;
  <a href="#how-it-works">How it works</a> &middot;
  <a href="#verifying-signed-webhooks">Signed webhooks</a> &middot;
  <a href="#configuration">Configuration</a> &middot;
  <a href="https://sarathsp06.github.io/testhooks/">Docs</a> &middot;
  <a href="https://testhooks.vercel.app/">Live demo</a>
</p>

Full documentation: **https://sarathsp06.github.io/testhooks/** · Try it without installing: **[live demo](https://testhooks.vercel.app/)**

---

## Quick start

Pick one. Each is a single command.

**Binary, no database** (quickest: nothing else to install; Linux, macOS, Windows; checks the SHA-256):

```bash
curl -fsSL https://raw.githubusercontent.com/sarathsp06/testhooks/main/install.sh | sh && DATABASE_URL=memory testhooks
```

**Docker, no database:**

```bash
docker run -d --name testhooks -p 8080:8080 -e DATABASE_URL=memory ghcr.io/sarathsp06/testhooks
```

**Docker, no database, data kept across restarts** (snapshot file in a volume):

```bash
docker run -d --name testhooks -p 8080:8080 -v testhooks-data:/data -e DATABASE_URL=memory -e SNAPSHOT_PATH=/data/testhooks.snapshot ghcr.io/sarathsp06/testhooks
```

**Docker Compose with PostgreSQL** (shared or long-running instances):

```bash
curl -fsSL https://raw.githubusercontent.com/sarathsp06/testhooks/main/deploy/docker-compose.yml | docker compose -p testhooks -f - up -d
```

Then open **http://localhost:8080**, create an endpoint, and send it a request:

```bash
curl -X POST http://localhost:8080/h/<slug> -H 'Content-Type: application/json' -d '{"hello":"world"}'
```

Notes:

- Compose options go in front of the command, e.g. `TESTHOOKS_PORT=9000 AUTH_TOKEN=change-me curl -fsSL … | docker compose -p testhooks -f - up -d`.
- Stop Compose with `docker compose -p testhooks down` (add `-v` to delete the data); stop the container with `docker rm -f testhooks`.
- The installer uses `/usr/local/bin` if writable, else `~/.local/bin` (run `~/.local/bin/testhooks` if that isn't on your `PATH`).
- Pin a release with `TESTHOOKS_VERSION=v1.1.0` in front of `sh`.
- In-memory mode is for one person testing locally. Add `SNAPSHOT_PATH=./testhooks.snapshot` to keep data across restarts (saved every 30 s and on shutdown).
- With PostgreSQL, migrations run on startup and the server waits up to 60 s for the database.

### From source

Prerequisites: Go 1.26+ and Node 22+. PostgreSQL is optional; without `DATABASE_URL` the binary stores data in memory.

```bash
git clone https://github.com/sarathsp06/testhooks.git && cd testhooks
make build && ./testhooks                       # single binary with the UI embedded; in memory by default
docker compose up --build                       # or: app + Postgres, built from this checkout
```

`make build-all` cross-compiles for Linux, macOS and Windows (amd64 and arm64) into `dist/`.

---

## Why testhooks?

- **No signup, no SaaS.** Run it on your own infra in under a minute.
- **Test webhooks against localhost without ngrok.** Webhooks hit the public server, stream to your browser over WebSocket, and your browser forwards them to `localhost` with `fetch()`. No CLI, daemon or tunnel.
- **Private when you need it.** In browser mode, payloads are relayed and never written to the server's disk.
- **Always on when you need it.** In server mode, everything is stored in Postgres, and transforms and forwarding run headlessly with no browser open.
- **Small footprint.** One Go binary with the Svelte UI embedded. Storage is PostgreSQL, or memory for local testing.

---

## Features

- **Unique webhook URLs:** each endpoint gets `/h/a1b2c3d4`; sub-paths like `/h/a1b2c3d4/any/path` are captured too.
- **Real-time streaming:** requests are pushed to the browser instantly over WebSocket.
- **Two endpoint modes:** server mode (stored, always on) or browser mode (relayed, not stored).
- **Forwarding:** from the server to public URLs (async or sync, with retries), or from the browser to anything, `localhost` included.
- **Transforms:** reshape requests with JavaScript on the server, or JavaScript, Lua and Jsonnet in the browser.
- **Custom responses:** a script sets the status, headers and body the sender gets back.
- **Signature verification:** [Standard Webhooks](https://www.standardwebhooks.com) (Sparrow, Svix and others), HMAC and Ed25519.
- **Export and replay:** download requests as JSON or CSV, or copy any request as cURL.
- **Protection:** optional token auth, per-IP rate limiting, body and storage limits, automatic pruning.
- **Storage:** PostgreSQL, or in memory with an optional snapshot file.

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
                              PostgreSQL or memory
```

**Server mode** stores every request (in PostgreSQL, or in memory), runs the transform on the server, forwards to the configured URL and returns the custom response. The browser is only a viewer, so it keeps working with no tab open.

**Browser mode** makes the server a thin relay: it receives the webhook, streams it over WebSocket and keeps only a short in-memory buffer so a reconnecting tab can catch up. Transforms and forwarding run in your browser, which is how it reaches `localhost`. Turn on *Persist requests* to keep a server-side copy as well.

The server is required in both modes because something public has to receive the webhook. A static build of the UI alone can't accept incoming HTTP requests.

### Responses webhook senders see

- `200`: captured in server mode, or whatever your custom response or sync forward target returned.
- `202`: captured and relayed in browser mode.
- `401`: signature verification is enforced and the signature is missing or invalid.
- `404`: no endpoint with that slug.
- `413`: body larger than `MAX_BODY_SIZE`.
- `429`: rate limited (`Retry-After: 1`).
- `503`: database unavailable; senders that retry on 5xx will redeliver.

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

Everything is configured with environment variables. A `.env` file in the working directory is loaded if present; see [`.env.example`](.env.example). Full reference: [docs](https://sarathsp06.github.io/testhooks/reference/configuration/).

Storage:

- `DATABASE_URL`: `memory`, or a PostgreSQL URL such as `postgres://user:pass@host:5432/testhooks`. Default `memory`.
- `SNAPSHOT_PATH`: with `memory`, file saved every 30 s and on shutdown, loaded on start. Default empty (memory only).

Server and security:

- `LISTEN`: bind address. Default `:8080`.
- `PORT`: if set, listen on `:<PORT>` instead (PaaS convention).
- `AUTH_TOKEN`: token required by the API and WebSocket; the UI prompts for it. `/h/*` and `/healthz` stay public.
- `ALLOWED_ORIGINS`: comma-separated CORS and WebSocket origins. Default `*`; set it in production.
- `ALLOWED_HEADERS`: CORS request headers. Default `Content-Type,Authorization,X-Requested-With`.
- `TRUSTED_PROXIES`: comma-separated CIDRs allowed to set `X-Forwarded-For` / `X-Real-IP`. If every request shows `127.0.0.1`, use `127.0.0.1/32,::1/128` (automatic on Vercel).
- `RATE_LIMIT_RPS`: per-IP requests/second on capture and API routes; `0` disables. Default `20`.
- `RATE_LIMIT_BURST`: per-IP burst size. Default `40`.
- `API_TIMEOUT_SECONDS`: REST API request timeout. Default `30`.

Limits:

- `MAX_BODY_SIZE`: max captured body in bytes; larger gets `413`. Default `524288` (512 KB).
- `MAX_REQUESTS_PER_ENDPOINT`: oldest requests beyond this are pruned. Default `500`.
- `MAX_ENDPOINT_STORAGE_BYTES`: oldest requests beyond this many bytes per endpoint are pruned. Default `10485760`.
- `PRUNE_INTERVAL_SECONDS`: how often pruning runs. Default `60`.

Browser mode and WebSocket:

- `RING_BUFFER_SIZE`: requests buffered per browser-mode endpoint for reconnecting tabs. Default `100`.
- `RING_BUFFER_TTL_SECONDS`: how long buffered requests are kept. Default `300`.
- `MAX_SUBSCRIBERS_PER_SLUG`: max concurrent WebSocket viewers per endpoint. Default `50`.

Development:

- `DEV`: proxy the UI to the Vite dev server. Default `false`.
- `VITE_URL`: Vite dev server address when `DEV=true`. Default `http://localhost:5173`.

### Running in production

- Use PostgreSQL; in-memory mode is for local testing.
- Run it as one long-running server or container (a VPS, Fly.io, Railway, Render), not on serverless functions such as Vercel: WebSockets don't stay open there, and in-memory endpoints vanish between instances.
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
