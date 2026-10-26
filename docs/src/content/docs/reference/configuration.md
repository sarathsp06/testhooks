---
title: Configuration
description: Every environment variable Testhooks reads.
---

Testhooks is configured entirely with environment variables. A `.env` file in the working directory is loaded if present; see [`.env.example`](https://github.com/sarathsp06/testhooks/blob/main/.env.example).

## Server

| Variable | Default | Description |
|---|---|---|
| `DATABASE_URL` | `memory` | `memory` keeps data in memory (local testing, no database). Set a PostgreSQL connection string, such as `postgres://user:pass@host:5432/testhooks`, for shared or long-running instances. See [Run without a database](../../getting-started/installation/#run-without-a-database) |
| `SNAPSHOT_PATH` | | With `DATABASE_URL=memory`: file the data is saved to every 30 seconds and on shutdown, and loaded from on start. Empty keeps data in memory only |
| `LISTEN` | `:8080` | Address to listen on |
| `PORT` | | If set, listens on `:<PORT>` instead of `LISTEN` (PaaS convention) |
| `API_TIMEOUT_SECONDS` | `30` | Timeout for REST API requests |

## Security

| Variable | Default | Description |
|---|---|---|
| `AUTH_TOKEN` | | Shared password for everything that reads or changes data: the API and the live view. The UI prompts for it; capture URLs (`/h/`) and `/healthz` stay public. Without it, anyone who reaches the server can read and change everything. See [Authentication](../../deployment/production/#authentication) |
| `ALLOWED_ORIGINS` | `*` | Comma-separated origins allowed for CORS and WebSocket connections |
| `ALLOWED_HEADERS` | `Content-Type,Authorization,X-Requested-With` | Comma-separated CORS request headers |
| `TRUSTED_PROXIES` | | Comma-separated CIDRs allowed to set `X-Forwarded-For` and `X-Real-IP`. On Vercel (`VERCEL=1`) it defaults to loopback, `127.0.0.1/32,::1/128` |
| `STRIP_HEADERS` | `default` | Comma-separated headers removed from captured requests before they are stored, shown or forwarded. A trailing `*` matches a prefix (`X-Internal-*`). `default` expands to Vercel's credentials: `X-Vercel-Oidc-Token`, `X-Vercel-Proxy-Signature`, `X-Vercel-Proxy-Signature-Ts` and `X-Vercel-Sc-*`, plus `Forwarded` on Vercel. Combine them, as in `default,X-Internal-*`. Empty or `none` strips nothing |
| `ENCRYPTION_KEYS` | | Comma-separated key encryption keys for signature secrets stored at rest, each `<key-id>=<64 hex characters>`. Generate one with `openssl rand -hex 32`. Required to save a secret with PostgreSQL or `SNAPSHOT_PATH`; in-memory storage without a snapshot uses a per-process key. See [How secrets are stored](../../guides/signatures/#how-secrets-are-stored) |
| `ENCRYPTION_PRIMARY_KEY_ID` | | The key in `ENCRYPTION_KEYS` that encrypts new secrets. Required when there is more than one key |
| `RATE_LIMIT_RPS` | `20` | Sustained requests per second per client IP on capture and API routes. `0` disables rate limiting |
| `RATE_LIMIT_BURST` | `40` | Burst size per client IP |

## Storage

| Variable | Default | Description |
|---|---|---|
| `MAX_BODY_SIZE` | `524288` | Maximum captured body in bytes. Larger requests get `413` |
| `MAX_REQUESTS_PER_ENDPOINT` | `500` | Oldest requests beyond this count are pruned |
| `MAX_ENDPOINT_STORAGE_BYTES` | `10485760` | Oldest requests beyond this many stored bytes per endpoint are pruned |
| `MAX_STORED_RESPONSE_SIZE` | `65536` | Bytes of a reply body stored with each request: the forward target's reply and the response sent back. Longer bodies are cut and marked truncated; the live view still shows them whole. Not counted in `MAX_ENDPOINT_STORAGE_BYTES`. `0` stores status and headers only; at most 10 MB |
| `PRUNE_INTERVAL_SECONDS` | `60` | How often pruning runs |
| `UPGRADE_NOTE` | | A sentence shown under the storage limits in the UI saying what a paid plan would lift, such as `A paid plan would keep 10,000 requests for 30 days.` It comes with an "I'd pay for that" button; the server counts one click per browser (see [Limits](../api/#limits)). Empty shows the limits only, so self-hosted installs see no mention of a paid plan |

## Browser mode and WebSocket

| Variable | Default | Description |
|---|---|---|
| `RING_BUFFER_SIZE` | `100` | Requests buffered in memory per browser-mode endpoint for reconnecting tabs |
| `RING_BUFFER_TTL_SECONDS` | `300` | How long buffered requests are kept |
| `MAX_SUBSCRIBERS_PER_SLUG` | `50` | Maximum concurrent WebSocket viewers per endpoint |

## Development

| Variable | Default | Description |
|---|---|---|
| `DEV` | `false` | Proxy the UI to the Vite dev server instead of serving the embedded build |
| `VITE_URL` | `http://localhost:5173` | Vite dev server address when `DEV=true` |
