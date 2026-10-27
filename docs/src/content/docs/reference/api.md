---
title: REST API
description: Manage endpoints and captured requests over HTTP.
---

The UI is built on this API, so anything it does you can script. All bodies are JSON. When [`AUTH_TOKEN`](../configuration/#security) is set, send `Authorization: Bearer <token>`.

Errors look like `{"error": "endpoint not found"}`. A missing record is `404`; a database failure is `500`.

## Endpoints

### Create an endpoint

`POST /api/endpoints`

```bash
curl -X POST http://localhost:8080/api/endpoints \
  -H 'Content-Type: application/json' \
  -d '{"name":"stripe test","slug_name":"stripe","mode":"server"}'
```

| Field | | |
|---|---|---|
| `name` | optional | A label, up to 255 characters. Doesn't change the URL |
| `slug_name` | optional | Start of the URL: up to 20 lowercase letters, numbers and single dashes, no dash at the start or end. Uppercase is lowercased |
| `mode` | optional | `server` (default) or `browser` |
| `config` | optional | See [endpoint config](#endpoint-config) |

Returns `201` with the endpoint:

```json
{
  "id": "251982e0-021a-4b2e-a8fd-50e505e0bd37",
  "slug": "stripe-acid-fable-gully",
  "name": "stripe test",
  "mode": "server",
  "created_at": "2026-10-16T23:03:47.672606Z",
  "config": {}
}
```

The webhook URL is `/h/<slug>`. The slug is `slug_name` followed by three random short words, or just the three words without a `slug_name`, so two endpoints never clash and a slug never collides with a page of the app. The words come from the [EFF short word list](https://www.eff.org/dice). They make the URL easy to read, not secret: anyone who has it can send to it. Endpoints created before readable slugs keep their 8-character slugs.

A `slug_name` that isn't allowed gets `400` with `{"error": "Use up to 20 lowercase letters, numbers and single dashes, with no dash at the start or end."}`. In the rare case that every slug tried is already taken, the response is `409`; send the request again.

An optional `X-Client-ID` header tags the endpoint so `GET /api/endpoints` with the same header lists only that client's endpoints. It's a convenience for the UI, not access control.

### List endpoints

`GET /api/endpoints?limit=50&offset=0`

Returns `{"endpoints": [...], "limit": 50, "offset": 0}`. `limit` is at most 200.

### Get an endpoint

`GET /api/endpoints/{id}` or `GET /api/slug/{slug}`

### Update an endpoint

`PATCH /api/endpoints/{id}`

```bash
curl -X PATCH http://localhost:8080/api/endpoints/<id> \
  -H 'Content-Type: application/json' \
  -d '{"mode":"browser"}'
```

Send any of `name`, `slug_name`, `mode` and `config`. Fields you leave out are unchanged, but `config` is replaced as a whole: include every setting you want to keep.

#### Change the URL

Sending `slug_name` gives the endpoint a new slug: that name followed by three new random words, or only the words when `slug_name` is `""`. The words change even when the name is the same.

```bash
curl -X PATCH http://localhost:8080/api/endpoints/<id> \
  -H 'Content-Type: application/json' \
  -d '{"slug_name":"orders"}'
```

The old URL stops working at once: senders still using it get `404` until you update them. Captured requests, settings and the endpoint `id` stay the same. The response has the new `slug`; the same `400` and `409` as creating apply.

### Delete an endpoint

`DELETE /api/endpoints/{id}` deletes the endpoint and all its requests. Returns `204`.

## Requests

### List requests

`GET /api/endpoints/{id}/requests?limit=50&offset=0`

Returns `{"requests": [...], "total": 123, "limit": 50, "offset": 0}`, newest first. `limit` is at most 200.

### Get a request

`GET /api/requests/{reqId}`

```json
{
  "id": "299e2e41-2c91-4baf-bc2d-9e38034667fe",
  "endpoint_id": "251982e0-021a-4b2e-a8fd-50e505e0bd37",
  "method": "POST",
  "path": "/",
  "headers": { "Content-Type": ["application/json"] },
  "query": { "debug": ["1"] },
  "body": "{\"hello\":\"world\"}",
  "body_encoding": "text",
  "content_type": "application/json",
  "ip": "203.0.113.7",
  "size": 17,
  "created_at": "2026-10-16T23:04:01.12Z",
  "signature": { "status": "verified", "scheme": "v1", "webhook_id": "msg_123" },
  "forward_result": {
    "request_id": "299e2e41-2c91-4baf-bc2d-9e38034667fe",
    "url": "https://example.com/hook",
    "status_code": 202,
    "ok": true,
    "latency_ms": 84,
    "response_body": "{\"received\":true}",
    "response_headers": { "Content-Type": "application/json" },
    "content_type": "application/json"
  },
  "response_info": {
    "request_id": "299e2e41-2c91-4baf-bc2d-9e38034667fe",
    "status": 200,
    "body": "{\"status\":\"ok\",\"id\":\"299e2e41-2c91-4baf-bc2d-9e38034667fe\"}",
    "content_type": "application/json",
    "source": "default"
  }
}
```

- `body` is plain text when it looks like text (`body_encoding: "text"`), otherwise base64 (`body_encoding: "base64"`).
- `signature` appears only when [signature verification](../../guides/signatures/) is configured.
- `forward_result` is the forward target's reply and `response_info` the response sent back to the caller, in the same shape as the [`forward_result` and `response_info` WebSocket messages](../websocket/). They're stored for server-mode requests (and browser-mode ones with `persist_requests`). An async forward fills in `forward_result` when it finishes, so it can be missing just after capture. Bodies longer than [`MAX_STORED_RESPONSE_SIZE`](../configuration/) are cut, with `response_body_truncated` or `body_truncated` set.

### Delete requests

- `DELETE /api/requests/{reqId}` deletes one request.
- `DELETE /api/endpoints/{id}/requests` deletes all of an endpoint's requests.

## Limits

### Read the storage limits

`GET /api/limits`

```json
{
  "max_requests_per_endpoint": 500,
  "max_endpoint_storage_bytes": 10485760,
  "max_body_size": 524288,
  "upgrade_note": "A paid plan would keep 10,000 requests for 30 days."
}
```

The three numbers are the server's [`MAX_REQUESTS_PER_ENDPOINT`, `MAX_ENDPOINT_STORAGE_BYTES` and `MAX_BODY_SIZE`](../configuration/#storage). The UI shows them next to the stored request count. `upgrade_note` appears only when [`UPGRADE_NOTE`](../configuration/#storage) is set.

### Say you would pay for more

`POST /api/upgrade-interest`

```bash
curl -X POST http://localhost:8080/api/upgrade-interest \
  -H 'Content-Type: application/json' \
  -H 'X-Client-ID: my-browser' \
  -d '{"endpoint_id":"251982e0-021a-4b2e-a8fd-50e505e0bd37"}'
```

This is the "I'd pay for that" button. `endpoint_id` is optional and records where the click happened. One `X-Client-ID` counts once however often it is sent; a call without one counts once per call. Returns `{"count": 3}`, the number of people who have asked so far. With PostgreSQL the rows are in the `upgrade_interest` table; with in-memory storage they live in the snapshot file.

## Endpoint config

| Field | | |
|---|---|---|
| `forward_url` | string | [Forward](../../guides/forwarding/) target |
| `forward_mode` | `async` \| `sync` | Defaults to `async`. With `sync` the sender gets the forward target's reply; in browser mode that is your local app's reply, relayed by the tab |
| `wasm_script` | string | Server-side JavaScript [transform](../../guides/scripts/) |
| `transform_language` | `javascript` | Language of the transform. Older `lua` and `jsonnet` scripts are kept but not run |
| `custom_response.enabled` | bool | Turn the response handler on |
| `custom_response.script` | string | Response handler script |
| `custom_response.language` | `javascript` | Language of the handler. Older `lua` and `jsonnet` scripts are kept but not run |
| `persist_requests` | bool | Store browser-mode requests in the database too |
| `signature.secret` | string | HMAC secret for [signature verification](../../guides/signatures/). Write-only: omit it to keep the stored secret, send `""` to remove it. Never returned |
| `signature.secret_set` | bool | Returned instead of the secret when one is stored. Ignored on input |
| `signature.public_key` | string | Hex Ed25519 public key |
| `signature.enforce` | bool | Reject invalid signatures with `401` |

## Capture

Any method to `/h/{slug}` or `/h/{slug}/{path...}` is captured. See [Status codes](../status-codes/) for what the sender gets back.

## Health

`GET /healthz` returns `200 {"status":"ok"}`, or `503` when the database is unreachable. It never requires authentication.
