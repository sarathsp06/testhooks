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
- **Run it as a long-running server, not serverless functions.** Testhooks keeps WebSocket connections open for the live view and browser mode, and in-memory storage only exists inside one process. Serverless platforms such as Vercel or AWS Lambda break both: webhook URLs disappear between instances and live updates never connect. Use a VPS or a container host (Fly.io, Railway, Render and similar).

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
