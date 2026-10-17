---
title: Forwarding
description: Relay captured webhooks to another URL, asynchronously or synchronously.
---

Set a **Target URL** under **Forwarding** in an endpoint's **Settings** tab and every captured request is forwarded there, after any [transform](../scripts/).

## Where forwarding runs

- **Server mode:** the Go server forwards. It runs for every request, even with no tab open, and only reaches public addresses.
- **Browser mode:** your tab forwards with `fetch()`. It can reach `localhost`, but only while the tab is open. See [Webhooks to localhost](../localhost/).

## Async and sync

**Async** (the default) is fire and forget. The sender gets its response immediately and the forward happens in the background.

**Sync** waits for the target to respond, then:

- with no response handler, sends the target's status, content type and body back to the original sender, so Testhooks behaves like a transparent proxy;
- with a response handler, passes the target's response to it as `req.forward_response`. See [Transforms and custom responses](../scripts/#custom-responses).

If a sync forward gets no response at all, the sender gets `502 Bad Gateway`.

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

Use browser mode to reach private addresses.

## Graceful shutdown

On shutdown the server waits up to 10 seconds for in-flight async forwards to finish, so redeploys don't silently drop them.
