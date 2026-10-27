---
title: Quickstart
description: Run Testhooks and capture your first webhook in a couple of minutes.
---

Just want to look around first? Try the [live demo](https://testhooks.vercel.app/), no install needed.

## 1. Install testhooks

```bash
curl -fsSL https://raw.githubusercontent.com/sarathsp06/testhooks/main/install.sh | sh
```

This downloads the binary for your OS and architecture, checks its SHA-256 and puts `testhooks` in `/usr/local/bin`, or in `~/.local/bin` if that isn't writable. You only do this once.

## 2. Run it

```bash
DATABASE_URL=memory testhooks
```

That's how you start it from now on: run `testhooks`. `DATABASE_URL=memory` keeps everything in memory, so you don't need a database. Open [http://localhost:8080](http://localhost:8080).

:::tip[Variations]
- **Keep data across restarts:** `DATABASE_URL=memory SNAPSHOT_PATH=./testhooks.snapshot testhooks`
- **Use PostgreSQL:** `DATABASE_URL='postgres://user:pass@host:5432/testhooks' testhooks`
- **`command not found`?** The installer used `~/.local/bin`, which isn't on your `PATH`. Run `~/.local/bin/testhooks`, or add that folder to your `PATH`.
- **Check the version:** `testhooks --version`
:::

### Or run it with Docker

One container, no database:

```bash
docker run -d --name testhooks -p 8080:8080 -e DATABASE_URL=memory ghcr.io/sarathsp06/testhooks
```

With PostgreSQL, for a shared instance:

```bash
curl -fsSL https://raw.githubusercontent.com/sarathsp06/testhooks/main/deploy/docker-compose.yml | docker compose -p testhooks -f - up -d
```

Check it's up:

```bash
curl http://localhost:8080/healthz
```

## 3. Create an endpoint

Open `http://localhost:8080` and click **Create endpoint**. You get a URL of three random words, like:

```
http://localhost:8080/h/acid-fable-gully
```

To make it easier to tell apart, fill in **Start the URL with**, for example `stripe`, and the URL becomes `/h/stripe-acid-fable-gully`. You can change the URL later under **Settings**, **Webhook URL**; senders using the old URL will get 404 until you update them.

Or create one with the API:

```bash
curl -X POST http://localhost:8080/api/endpoints \
  -H 'Content-Type: application/json' \
  -d '{"name":"my first endpoint","slug_name":"stripe","mode":"server"}'
```

The response includes the `slug` used in the webhook URL.

## 4. Send it a webhook

```bash
curl -X POST http://localhost:8080/h/acid-fable-gully \
  -H 'Content-Type: application/json' \
  -d '{"event":"order.created","order_id":"ord_123"}'
```

The request appears in the UI immediately, with its headers, query and body. Anything after the slug is captured too, so `/h/acid-fable-gully/stripe/events` works.

## 5. Point a real provider at it

Providers such as Stripe, GitHub or Slack need a public URL. Run Testhooks somewhere public (see [Running in production](../../deployment/production/)) and paste the endpoint URL into the provider's webhook settings.

## Next steps

- Send webhooks on to your local app without a tunnel: [Webhooks to localhost](../../guides/localhost/)
- Forward to another service: [Forwarding](../../guides/forwarding/)
- Reshape payloads or script the response: [Transforms and custom responses](../../guides/scripts/)
- Check signatures from Sparrow, Svix and others: [Verifying signed webhooks](../../guides/signatures/)

Stop the binary with Ctrl+C, the single container with `docker rm -f testhooks`, or the Compose setup with `docker compose -p testhooks down` (add `-v` to delete the stored data too).
