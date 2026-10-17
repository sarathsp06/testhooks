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
| `AUTH_TOKEN` | | Require this token for the API and WebSocket. The UI prompts for it; capture URLs and `/healthz` stay public. See [Authentication](../../deployment/production/#authentication) |
| `ALLOWED_ORIGINS` | `*` | Comma-separated origins allowed for CORS and WebSocket connections |
| `ALLOWED_HEADERS` | `Content-Type,Authorization,X-Requested-With` | Comma-separated CORS request headers |
| `TRUSTED_PROXIES` | | Comma-separated CIDRs allowed to set `X-Forwarded-For` and `X-Real-IP` |
| `RATE_LIMIT_RPS` | `20` | Sustained requests per second per client IP on capture and API routes. `0` disables rate limiting |
| `RATE_LIMIT_BURST` | `40` | Burst size per client IP |

## Storage

| Variable | Default | Description |
|---|---|---|
| `MAX_BODY_SIZE` | `524288` | Maximum captured body in bytes. Larger requests get `413` |
| `MAX_REQUESTS_PER_ENDPOINT` | `500` | Oldest requests beyond this count are pruned |
| `MAX_ENDPOINT_STORAGE_BYTES` | `10485760` | Oldest requests beyond this many stored bytes per endpoint are pruned |
| `PRUNE_INTERVAL_SECONDS` | `60` | How often pruning runs |

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
