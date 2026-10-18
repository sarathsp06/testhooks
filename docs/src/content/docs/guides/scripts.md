---
title: Transforms and custom responses
description: Reshape requests before forwarding, and script the response returned to the sender.
---

Two kinds of script can run on each request, both edited in the endpoint's **Transform** tab:

- A **transform** changes the request before it's forwarded, or skips it.
- A **response handler** decides the status, headers and body returned to the webhook sender.

Scripts are JavaScript. They run sandboxed in WebAssembly (QuickJS), on the server in server mode and in your browser tab in browser mode. Both load the same helpers, so a script behaves the same in either mode and in the editor's **Test** button.

- Each run gets a fresh runtime, so nothing carries over between requests.
- A script has 5 seconds to finish.
- On the server, memory is capped at 64 MB and the result at 1 MB.

## What a script gets

Both kinds of script receive the same `req`:

```js
req.method          // "POST"
req.path            // "/stripe/events", the part after /h/<slug>
req.query           // { debug: ["1"] }, values are arrays
req.headers         // { "Content-Type": ["application/json"] }, values are arrays
req.body            // the raw body, always a string
req.json            // the body parsed as JSON, or null when it isn't JSON
req.content_type    // "application/json"

req.header("x-event")             // first value of a header, any case; undefined if absent
req.setHeader("x-source", "test") // replace a header
req.removeHeader("x-signature")   // delete a header
```

## Transforms

Define `transform(req)` and return the request to forward:

```js
function transform(req) {
  if (req.json) {
    req.json.received_at = new Date().toISOString();
    delete req.json.internal_id;
  }
  return req;
}
```

- Changes to `req.json` are written back to the body. If you don't touch `req.json`, the body is forwarded byte for byte.
- You can also set `req.body` yourself, or return a new object. Fields you leave out keep their original values.
- Setting `req.json` on a request that wasn't JSON sets the content type to `application/json`, unless you set one.

### Skipping a request

Return `null` to capture a request without forwarding it:

```js
function transform(req) {
  if (req.json?.type === 'ping') return null;
  return req;
}
```

### When something goes wrong

If a transform throws, times out or returns nothing, the original request is forwarded unchanged and the error is logged. Forgetting `return req` is the most common mistake; the **Test** button shows it as an error.

### Scripts from older versions

Earlier versions also ran Lua and Jsonnet in browser mode. Those scripts are kept with the endpoint but no longer run. The Transform tab shows them read-only with a button to start a JavaScript version.

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

- `status` defaults to `200`.
- `headers` is a flat object of strings.
- `body` is a string, empty by default.
- `content_type` defaults to `text/plain`.

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
