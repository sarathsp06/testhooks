---
title: Transforms and custom responses
description: Reshape requests before forwarding, and script the response returned to the sender.
---

Two kinds of script can run on each request, both edited in the endpoint's **Transform** tab:

- A **transform** reshapes the request before it is forwarded.
- A **response handler** decides the status, headers and body returned to the webhook sender.

Scripts run sandboxed in WebAssembly. Each execution gets a fresh runtime, so no state carries over between requests.

| | Server mode | Browser mode |
|---|---|---|
| Languages | JavaScript (QuickJS) | JavaScript, Lua, Jsonnet |
| Time limit | 5 seconds | 5 seconds |
| Memory limit | 64 MB | Browser's limit |
| Max output | 1 MB | |

## The request object

Both kinds of script receive the same `req` object:

```js
{
  method: "POST",
  path: "/stripe/events",            // the part after /h/<slug>
  headers: { "Content-Type": ["application/json"] },   // values are arrays
  query: { "debug": ["1"] },
  body: "{\"event\":\"order.created\"}", // always a string; parse it yourself
  content_type: "application/json"
}
```

## Transforms

### JavaScript

Define `transform(req)` and return the request to forward. Any field you leave out keeps its original value.

```js
function transform(req) {
  const body = JSON.parse(req.body || '{}');
  body.received_at = new Date().toISOString();
  return { ...req, body: JSON.stringify(body) };
}
```

On the server, return `{ drop: true }` to capture a request without forwarding it:

```js
function transform(req) {
  const body = JSON.parse(req.body || '{}');
  if (body.type === 'ping') return { drop: true };
  return req;
}
```

### Lua (browser mode)

```lua
function transform(req)
  local body = json.decode(req.body or "{}")
  body.source = "testhooks"
  req.body = json.encode(body)
  return req
end
```

`json.decode` and `json.encode` are available.

### Jsonnet (browser mode)

The request arrives as an external variable and the script evaluates to the new request:

```jsonnet
local req = std.parseJson(std.extVar("req"));
local body = std.parseJson(req.body);

req { body: std.manifestJsonEx(body { source: "testhooks" }, "  ") }
```

If a transform throws or times out, the original request is forwarded unchanged and the error is logged.

## Custom responses

Enable the response handler and define `handler(req)`. It runs after the transform and the forward, and returns the response for the sender:

```js
function handler(req) {
  return {
    status: 200,
    headers: { "X-Powered-By": "Testhooks" },
    body: JSON.stringify({ ok: true, received: req.method })
  };
}
```

| Field | Default |
|---|---|
| `status` | `200` |
| `headers` | none |
| `body` | empty |
| `content_type` | `text/plain` |

Return `null` to fall back to the default response. If the handler fails, the default response is used.

Use it to test how your sender copes with errors, without changing your app:

```js
function handler(req) {
  if (req.query.fail) return { status: 500, body: "simulated failure" };
  return { status: 200, body: "ok" };
}
```

### Using the forward target's response

With **sync** [forwarding](../forwarding/#async-and-sync), the target's response is available as `req.forward_response`:

```js
function handler(req) {
  const upstream = req.forward_response;   // { status, headers, body, content_type }
  if (upstream.status >= 500) {
    return { status: 202, body: "queued for retry" };
  }
  return { status: upstream.status, body: upstream.body, content_type: upstream.content_type };
}
```

### Headers you can't set

For safety, scripts can't set security-sensitive response headers. These are ignored:
`Set-Cookie`, `Content-Security-Policy`, `Content-Security-Policy-Report-Only`, `X-Frame-Options`, `X-Content-Type-Options`, `Strict-Transport-Security`, `Permissions-Policy`, `Referrer-Policy`, `X-XSS-Protection`, `Access-Control-Allow-Origin`, `Access-Control-Allow-Credentials` and `Transfer-Encoding`.
