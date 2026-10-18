---
title: Server and browser mode
description: Where requests are stored and where transforms and forwarding run.
---

Every endpoint runs in one of two modes. You can switch at any time from the endpoint's **Settings** tab.

| | Server mode | Browser mode |
|---|---|---|
| Requests stored | In the database (PostgreSQL or memory) | Not stored (optional *Persist requests*) |
| Transforms run | On the server (JavaScript) | In your browser (JavaScript) |
| Forwarding runs | On the server, public URLs only | In your browser, any URL including `localhost` |
| Custom responses | Scripted on the server | Scripted in your browser while a tab is open |
| Works with no tab open | Yes | Captures are relayed only to open tabs |
| Response to the sender | `200` | `202` |

## Server mode

The default. Every request is stored (in PostgreSQL, or in memory with `DATABASE_URL=memory`), then the server runs the pipeline:

1. **Capture** the request.
2. **Store** it.
3. **Transform** it with your JavaScript, if configured.
4. **Forward** it to your target URL, if configured.
5. **Respond** to the sender with your custom response, the forward target's response, or the default.

The browser is only a viewer, so this keeps working around the clock. Use it for anything you want a durable record of, and for forwarding that must not depend on a tab being open.

Old requests are pruned automatically per endpoint once it passes `MAX_REQUESTS_PER_ENDPOINT` (default 500) or `MAX_ENDPOINT_STORAGE_BYTES` (default 10 MB).

## Browser mode

The server acts as a thin relay. It receives the webhook, streams it to your open tabs over WebSocket and answers the sender with `202 Accepted`. Your browser then runs the transform and forwards the result with `fetch()`, which is how it can reach `localhost`. See [Webhooks to localhost](../localhost/).

Nothing is written to disk. The server keeps a short in-memory buffer per endpoint (`RING_BUFFER_SIZE`, kept for `RING_BUFFER_TTL_SECONDS`) so a tab that reconnects can catch up on what it missed.

Turn on **Persist requests** in Settings to keep a server-side copy as well. Processing still happens in the browser.

### Custom responses in browser mode

When a browser-mode endpoint has a response handler and a tab is open, the server holds the sender's request open and asks the tab to compute the response. If no tab answers within 10 seconds, the sender gets the default `202`.

## Why a server is always needed

Webhook providers deliver over HTTP to a public URL, so something public has to receive them. A static build of the UI can't accept incoming requests. In browser mode the server's job shrinks to relaying, but it still has to be there.
