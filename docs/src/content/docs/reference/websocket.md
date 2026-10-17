---
title: WebSocket events
description: The live event stream the UI subscribes to.
---

Connect to `/ws/{slug}` to receive an endpoint's events as they happen. The UI uses this for its live view; you can use it to build your own tooling.

```js
const ws = new WebSocket('wss://hooks.example.com/ws/1a8832ef');
ws.onmessage = (e) => {
  const { type, data } = JSON.parse(e.data);
  if (type === 'request') console.log(data.method, data.path, data.body);
};
```

When `AUTH_TOKEN` is set, pass it as a subprotocol, since browsers can't set headers on WebSockets:

```js
new WebSocket(url, [`auth.${token}`]);
```

The server pings every 30 seconds. Up to `MAX_SUBSCRIBERS_PER_SLUG` clients (default 50) may connect per endpoint; beyond that, connections are closed with status `1013` (try again later).

## Messages from the server

Every message is `{"type": "...", "data": {...}}`.

| `type` | Sent when | `data` |
|---|---|---|
| `request` | A request is captured | The captured request, in the same shape as the [REST API](../api/#get-a-request) |
| `response_info` | The response to the sender is decided | `request_id`, `status`, `headers`, `body`, `content_type`, `source` |
| `forward_result` | A server-side forward finishes | `request_id`, `url`, `status_code`, `ok`, `latency_ms`, `error`, `response_body`, `response_headers`, `content_type` |
| `response_needed` | A browser-mode endpoint with a response handler needs the tab to compute a response | `request_id`, `request` |

`source` in `response_info` is one of `default`, `handler`, `forward_passthrough` or `signature` (rejected by signature enforcement).

Browser-mode endpoints buffer recent `request` messages in memory, so a client that reconnects within `RING_BUFFER_TTL_SECONDS` first receives what it missed.

## Messages to the server

Reply to `response_needed` within 10 seconds with:

```json
{
  "type": "response_result",
  "data": {
    "request_id": "…",
    "status": 200,
    "headers": { "X-Example": "1" },
    "body": "ok",
    "content_type": "text/plain"
  }
}
```

If no reply arrives in time, the sender gets the default `202` response. Other message types are ignored. Messages larger than 1 MB close the connection.
