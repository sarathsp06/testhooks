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
  -d '{"name":"stripe test","mode":"server"}'
```

| Field | | |
|---|---|---|
| `name` | optional | Up to 255 characters |
| `mode` | optional | `server` (default) or `browser` |
| `config` | optional | See [endpoint config](#endpoint-config) |

Returns `201` with the endpoint:

```json
{
  "id": "251982e0-021a-4b2e-a8fd-50e505e0bd37",
  "slug": "1a8832ef",
  "name": "stripe test",
  "mode": "server",
  "created_at": "2026-10-16T23:03:47.672606Z",
  "config": {}
}
```

The webhook URL is `/h/<slug>`.

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

Send any of `name`, `mode` and `config`. Fields you leave out are unchanged, but `config` is replaced as a whole: include every setting you want to keep.

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
  "signature": { "status": "verified", "scheme": "v1", "webhook_id": "msg_123" }
}
```

- `body` is plain text when it looks like text (`body_encoding: "text"`), otherwise base64 (`body_encoding: "base64"`).
- `signature` appears only when [signature verification](../../guides/signatures/) is configured.

### Delete requests

- `DELETE /api/requests/{reqId}` deletes one request.
- `DELETE /api/endpoints/{id}/requests` deletes all of an endpoint's requests.

## Endpoint config

| Field | | |
|---|---|---|
| `forward_url` | string | [Forward](../../guides/forwarding/) target |
| `forward_mode` | `async` \| `sync` | Defaults to `async` |
| `wasm_script` | string | Server-side JavaScript [transform](../../guides/scripts/) |
| `transform_language` | `javascript` | Language of the transform. Older `lua` and `jsonnet` scripts are kept but not run |
| `custom_response.enabled` | bool | Turn the response handler on |
| `custom_response.script` | string | Response handler script |
| `custom_response.language` | `javascript` | Language of the handler. Older `lua` and `jsonnet` scripts are kept but not run |
| `persist_requests` | bool | Store browser-mode requests in the database too |
| `signature.secret` | string | HMAC secret for [signature verification](../../guides/signatures/) |
| `signature.public_key` | string | Hex Ed25519 public key |
| `signature.enforce` | bool | Reject invalid signatures with `401` |

## Capture

Any method to `/h/{slug}` or `/h/{slug}/{path...}` is captured. See [Status codes](../status-codes/) for what the sender gets back.

## Health

`GET /healthz` returns `200 {"status":"ok"}`, or `503` when the database is unreachable. It never requires authentication.
