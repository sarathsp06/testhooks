---
title: Forwarding
description: Relay captured webhooks to another URL, asynchronously or synchronously.
---

Set a **Target URL** under **Forwarding** in an endpoint's **Settings** tab and every captured request is forwarded there, after any [transform](../scripts/).

## Where forwarding runs

- **Server mode:** the Go server forwards. It runs for every request, even with no tab open, and only reaches public addresses unless the server sets [`ALLOW_PRIVATE_FORWARD_TARGETS`](#blocked-destinations).
- **Browser mode:** your tab forwards with `fetch()`. It can reach `localhost`, but only while the tab is open. See [Webhooks to localhost](../localhost/).

## Async and sync

**Async** (the default) is fire and forget. The sender gets its response immediately and the forward happens in the background.

**Sync** waits for the target to respond, then:

- with no response handler, sends the target's status, headers and body back to the original sender, so Testhooks behaves like a transparent proxy;
- with a response handler, passes the target's response to it as `req.forward_response`. See [Transforms and custom responses](../scripts/#custom-responses).

If a sync forward gets no response at all, the sender gets `502 Bad Gateway`.

Sync works the same in both modes. In server mode the server waits for the target. In browser mode the server holds the sender's request open while your tab forwards and waits, which is how an app on `localhost` can answer the sender; the Settings tab shows this as **Reply to the sender with my app's response**. Hop-by-hop headers and `Content-Length` are dropped from the relayed reply, and so are security-sensitive headers such as `Set-Cookie`.

## What the server sends

- The original method, path and body, or the transformed ones.
- The original headers, minus hop-by-hop headers such as `Connection` and `Transfer-Encoding`.
- `X-Forwarded-By: testhooks`.

## Retries and limits (server mode)

| | |
|---|---|
| Timeout per attempt | 10 seconds |
| Retries | Up to 3, with exponential backoff and jitter |
| Retried on | Network errors, `5xx` and `429` |
| Total time budget | 30 seconds per request |
| Redirects | Not followed; the first response is reported |
| Response body kept | First 1 MB |
| Concurrent async forwards | 256; beyond that, forwards are skipped and the skip is reported in the UI |

The result of each forward, including status, latency, headers and body, is shown on the request in the UI.

## Blocked destinations

To stop the server from being used to reach internal systems (SSRF), server-side forwarding refuses to connect to private, loopback, link-local, carrier-grade NAT and cloud metadata addresses. The check happens after DNS resolution, so a public hostname pointing at a private IP is blocked too. Only `http` and `https` URLs are allowed.

A blocked forward is not retried. It shows on the request as:

> The target URL points to the private address 10.100.3.252, which this server does not forward to. Switch the endpoint to browser mode to forward from your own machine, or set ALLOW_PRIVATE_FORWARD_TARGETS=true if you run this server on the same network.

To reach a private address, either:

- **Switch the endpoint to browser mode.** Your tab forwards, so anything your machine can reach works, as long as the target answers CORS. See [Webhooks to localhost](../localhost/).
- **Set `ALLOW_PRIVATE_FORWARD_TARGETS=true` on a server you run yourself**, on your laptop or inside your network. Server-side forwarding can then reach private addresses (`10.x`, `172.16.x`–`172.31.x`, `192.168.x`, `fc00::/7`), carrier-grade NAT (`100.64.0.0/10`) and the server's own `localhost`. Link-local and cloud metadata addresses such as `169.254.169.254` stay blocked. Do not set it on a server the public can create endpoints on: anyone could then use it to send requests into your network. `GET /api/limits` reports the setting as `forward_private_addresses`, and the Settings tab stops refusing `localhost` targets in server mode.

## Graceful shutdown

On shutdown the server waits up to 10 seconds for in-flight async forwards to finish, so redeploys don't silently drop them.
