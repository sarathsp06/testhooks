---
title: Deploy on Vercel
description: Run Testhooks on Vercel with PostgreSQL, and what works differently there.
---

[![Deploy with Vercel](https://vercel.com/button)](https://vercel.com/new/clone?repository-url=https%3A%2F%2Fgithub.com%2Fsarathsp06%2Ftesthooks&project-name=testhooks&env=DATABASE_URL&envDescription=PostgreSQL%20connection%20string%20(session%20mode%20or%20direct%2C%20not%20a%20transaction%20pooler)&envLink=https%3A%2F%2Fsarathsp06.github.io%2Ftesthooks%2Fdeployment%2Fvercel%2F)

Testhooks runs on Vercel's Go runtime. The repository includes a `vercel.json` that selects the Go preset and builds the web UI before the binary, so there's nothing to configure except environment variables.

## Deploy

1. Click **Deploy with Vercel** above, or import the repository in the Vercel dashboard.
2. Set `DATABASE_URL` to a PostgreSQL connection string. See [Choose the database connection](#choose-the-database-connection).
3. Set `ENCRYPTION_KEYS` to `k1=` followed by the output of `openssl rand -hex 32`. It encrypts [signature secrets](../../guides/signatures/#how-secrets-are-stored) in the database; without it they can't be saved.
4. Set `AUTH_TOKEN` unless this is a deliberately public demo. Without it, anyone who finds the deployment can read every captured webhook and change or delete endpoints; see [Authentication](../production/#authentication). Also set `ALLOWED_ORIGINS` to your deployment's URL, such as `https://testhooks-yourname.vercel.app`.
5. Deploy. Migrations run automatically on the first request that starts an instance.

For a public demo, `UPGRADE_NOTE` adds a line under the storage limits saying what a paid plan would lift, with a button that counts who asks; see [Configuration](../../reference/configuration/#storage).

[Fluid compute](https://vercel.com/docs/fluid-compute) must be on, because the live view uses WebSockets. It's the default for projects created on or after 23 April 2025.

## Choose the database connection

Use PostgreSQL. In-memory storage doesn't work on Vercel: each instance has its own memory, so endpoints created on one instance are missing on the next.

Use a **session-mode** or **direct** connection, not a transaction-mode pooler:

- **Supabase:** use the session pooler (the pooler host on port `5432`) or the direct connection string, not the transaction pooler on port `6543`.
- **Other providers:** avoid PgBouncer in transaction mode.

Transaction-mode poolers hand each query to a different backend connection. The Postgres driver caches prepared statements per connection, so queries fail intermittently with errors like `prepared statement "stmtcache_…" already exists`, and migrations can leave a lock behind.

### Connection limits

Every Vercel instance opens its own connections, and a deploy briefly runs the old and new instances side by side. A session pooler caps clients for the whole project (Supabase's free tier allows 15), so on Vercel each instance uses at most 3 connections and closes idle ones after 30 seconds.

If startup logs `max clients reached in session mode`, the pooler is full. Lower the per-instance limit by adding `pool_max_conns` to the URL, for example `...postgres?pool_max_conns=2`, or raise the pool size in your database provider's settings. `pool_min_conns` and `pool_max_conn_idle_time` work the same way.

If a pasted connection string has a trailing newline, Testhooks trims it. A string that still can't be parsed stops startup with `invalid DATABASE_URL`, with the password redacted.

## What works differently on Vercel

**Client IPs** are recorded correctly with no setup. When `VERCEL=1` is set and `TRUSTED_PROXIES` isn't, Testhooks trusts Vercel's proxy and reads the client address from the headers Vercel sets. Vercel overwrites those headers, so clients can't spoof them.

**Vercel's credentials are stripped from captured requests.** Vercel adds an OIDC token for your project (`X-Vercel-Oidc-Token`) and a proxy signature (`X-Vercel-Proxy-Signature`, and a `sig` in `Forwarded`) to every request. Anyone who can see an endpoint could reuse them, so Testhooks removes them before storing, showing or forwarding a request. The `X-Vercel-Ip-*` location headers are kept. Change the list with [`STRIP_HEADERS`](../../reference/configuration/).

**WebSockets work, with reconnects.** Vercel closes a WebSocket when the function reaches its [maximum duration](https://vercel.com/docs/functions/limitations#max-duration), 300 seconds by default. The UI reconnects automatically.

**Live updates only reach browsers on the same instance.** Vercel can run several instances, and each WebSocket stays on the instance that accepted it. Testhooks pushes a captured request to the browsers connected to the instance that received it:

- **Server mode** works: every request is stored in PostgreSQL, along with the forward target's reply and the response sent back. While the tab is visible it checks the database every 5 seconds, and again when it reconnects or comes back into view, so a request that landed on another instance shows up within a few seconds without a reload.
- **Prefer sync forwarding.** An async forward runs after Testhooks has answered the sender, and Vercel may pause the instance once that response is sent, so the forward can be cut short. Sync forwarding finishes before the response.
- **Browser mode** is unreliable: requests aren't stored, so a webhook that lands on a different instance than your tab never reaches it, and browser-computed custom responses fall back to the default. Use server mode on Vercel, or run Testhooks as [one long-running server](../production/) for browser mode.

## Build details

`vercel.json` runs:

```bash
cd web && npm ci && npm run build && cd .. && go build -trimpath -ldflags="-s -w -X main.version=${VERCEL_GIT_COMMIT_SHA:-vercel}" -o "$VERCEL_OUTPUT_FILE" ./cmd/testhooks
```

The UI must be built first because it's embedded in the binary. Vercel reads the Go version from `go.mod`, and the server listens on the `PORT` Vercel provides.
