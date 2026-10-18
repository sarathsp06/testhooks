---
title: Running in production
description: Authentication, reverse proxies, health checks, limits and shutdown behaviour.
---

Testhooks has to be reachable from the internet for webhook providers to deliver to it. Lock down everything except the capture URLs.

## Checklist

- **Set `AUTH_TOKEN`.** Without it, anyone who can reach the server can list, read and delete every endpoint and request.
- **Set `ALLOWED_ORIGINS`** to the origin you serve the UI from, such as `https://hooks.example.com`.
- **Terminate TLS** in a reverse proxy or load balancer in front of Testhooks.
- **Set `TRUSTED_PROXIES`** to that proxy's CIDR so rate limiting and the captured client IP use the real client address.
- **Use PostgreSQL**, managed or backed up, if you need to keep server-mode requests. In-memory storage (the default) is for local testing: one process, data in RAM, no shared access.
- **Prefer one long-running server.** Live updates go from the process that received a webhook to the browsers connected to that same process, and in-memory storage exists only inside one process. A VPS or a single-instance container host (Fly.io, Railway, Render and similar) avoids both limits. On platforms that run several instances, such as Vercel, use PostgreSQL and expect live updates and browser mode to be unreliable; see [Deploy on Vercel](../vercel/).

## Authentication

With `AUTH_TOKEN` set, only the paths that carry data need the token:

| Path | Access |
|---|---|
| `/api/*` | `Authorization: Bearer <token>` |
| `/ws/*` | WebSocket subprotocol `auth.<token>`, since browsers can't set headers on WebSockets |
| `/h/*` (capture) | Public, since providers can't authenticate |
| `/healthz` | Public, for orchestrator probes |
| Everything else (the UI) | Public. The UI bundle holds no data, and it has to load to show the sign-in prompt |

When the UI gets a `401`, it asks for the token, checks it against the server and stores it in that browser's `localStorage`. Requests that were waiting retry automatically and the live view reconnects. If the token changes on the server, the UI says the saved token was rejected and asks again. Use the sign-out button in the header to forget the token.

Failed attempts are logged with the client IP.

## Behind a reverse proxy

Testhooks reads `X-Forwarded-For` and `X-Real-IP` only from peers listed in `TRUSTED_PROXIES`. Otherwise it uses the connection's address, so clients can't spoof their IP.

```bash
TRUSTED_PROXIES=10.0.0.0/8,172.16.0.0/12
```

If every captured request shows `127.0.0.1` (or `::1`) as its IP, your platform's proxy connects to Testhooks over loopback. On Vercel this is handled automatically: when `VERCEL=1` is set and `TRUSTED_PROXIES` is empty, loopback is trusted and the startup log says so. On other platforms, trust loopback yourself:

```bash
TRUSTED_PROXIES=127.0.0.1/32,::1/128
```

Without this, rate limiting also sees every visitor as one IP, so they all share a single per-IP budget.

Make sure the proxy supports WebSocket upgrades on `/ws/*` and doesn't time out idle WebSockets too quickly. Testhooks pings every 30 seconds.

## Health checks

`GET /healthz` returns `200 {"status":"ok"}` when the database is reachable and `503` when it isn't. Use it for liveness and readiness probes.

## Limits

| Limit | Default | Setting |
|---|---|---|
| Request body | 512 KB, larger gets `413` | `MAX_BODY_SIZE` |
| Rate per client IP | 20 req/s, burst 40, on capture and API routes | `RATE_LIMIT_RPS`, `RATE_LIMIT_BURST` |
| Stored requests per endpoint | 500 | `MAX_REQUESTS_PER_ENDPOINT` |
| Stored bytes per endpoint | 10 MB | `MAX_ENDPOINT_STORAGE_BYTES` |
| WebSocket viewers per endpoint | 50 | `MAX_SUBSCRIBERS_PER_SLUG` |
| API request timeout | 30 s | `API_TIMEOUT_SECONDS` |

The pruner deletes the oldest requests beyond these limits every `PRUNE_INTERVAL_SECONDS`.

## Startup and shutdown

- On startup, the server retries the database connection for up to 60 seconds, so it tolerates the database starting at the same time.
- On `SIGTERM` or `SIGINT` it stops accepting connections and gives in-flight requests and background forwards up to 10 seconds to finish. Give your orchestrator a termination grace period above that.

## Database failures

If PostgreSQL becomes unreachable, capture requests get `503 Service Unavailable`, not `404`. Most webhook senders retry `5xx` responses, so deliveries made during an outage are redelivered once the database is back.
