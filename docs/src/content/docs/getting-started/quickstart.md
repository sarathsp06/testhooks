---
title: Quickstart
description: Run Testhooks and capture your first webhook in a couple of minutes.
---

## 1. Start Testhooks

You need Docker. This runs Testhooks and PostgreSQL from the published image:

```bash
curl -fsSL https://raw.githubusercontent.com/sarathsp06/testhooks/main/deploy/docker-compose.yml | docker compose -p testhooks -f - up -d
```

Check it's up:

```bash
curl http://localhost:8080/healthz
# {"status":"ok"}
```

No Docker? Use the [binary installer](../../getting-started/installation/#install-the-binary) instead.

## 2. Create an endpoint

Open `http://localhost:8080` and click **Create New Endpoint**. You get a URL like:

```
http://localhost:8080/h/a1b2c3d4
```

Or create one with the API:

```bash
curl -X POST http://localhost:8080/api/endpoints \
  -H 'Content-Type: application/json' \
  -d '{"name":"my first endpoint","mode":"server"}'
```

The response includes the `slug` used in the webhook URL.

## 3. Send it a webhook

```bash
curl -X POST http://localhost:8080/h/a1b2c3d4 \
  -H 'Content-Type: application/json' \
  -d '{"event":"order.created","order_id":"ord_123"}'
```

The request appears in the UI immediately, with its headers, query and body. Anything after the slug is captured too, so `/h/a1b2c3d4/stripe/events` works.

## 4. Point a real provider at it

Providers such as Stripe, GitHub or Slack need a public URL. Run Testhooks somewhere public (see [Running in production](../../deployment/production/)) and paste the endpoint URL into the provider's webhook settings.

## Next steps

- Send webhooks on to your local app without a tunnel: [Webhooks to localhost](../../guides/localhost/)
- Forward to another service: [Forwarding](../../guides/forwarding/)
- Reshape payloads or script the response: [Transforms and custom responses](../../guides/scripts/)
- Check signatures from Sparrow, Svix and others: [Verifying signed webhooks](../../guides/signatures/)

Stop everything with `docker compose -p testhooks down`. Add `-v` to delete the stored data too.
