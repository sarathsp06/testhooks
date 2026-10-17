---
title: Verifying signed webhooks
description: Check Standard Webhooks signatures from Sparrow, Svix and other senders.
---

Many webhook senders sign each delivery so receivers can prove it is genuine. Testhooks verifies the [Standard Webhooks](https://www.standardwebhooks.com) format, used by [Sparrow](https://sarathsp06.github.io/sparrow/), Svix and others. Each request then shows whether its signature checked out, which is handy when building or debugging a receiver.

## How the scheme works

The sender adds three headers:

| Header | Value |
|---|---|
| `webhook-id` | A unique message ID, the same across retries of one delivery |
| `webhook-timestamp` | Unix seconds when it was sent |
| `webhook-signature` | One or more space-separated signatures, such as `v1,<base64> v1a,<base64>` |

The signed content is `{webhook-id}.{webhook-timestamp}.{raw body}`.

- `v1` is HMAC-SHA256 with a shared secret. A secret starting with `whsec_` is base64-decoded first; any other secret is used as raw bytes.
- `v1a` is Ed25519, checked with the sender's public key.

A request verifies if any signature of a configured type matches, and the timestamp is within 5 minutes of when it was received.

## Turn it on

In the endpoint's **Settings** tab, under **Signature Verification**:

1. Paste the sender's secret (`whsec_…`), its Ed25519 public key in hex, or both.
2. Save.

Each request in the list is now marked **signed** or **sig invalid**. The request detail shows the reason, such as `v1 signature does not match` or a timestamp outside tolerance, and the `webhook-id`, so retries of the same delivery are easy to spot.

Verification uses the time each request was received, so stored requests still show the right result later.

## Reject bad signatures

Turn on **Reject invalid signatures with 401** to make the endpoint answer like a real receiver: requests with a missing or invalid signature get `401 Unauthorized`.

```json
{ "error": "invalid webhook signature", "detail": "v1 signature does not match" }
```

Rejected requests are still captured, so you can see what was sent. Senders such as Sparrow treat `401` as permanent and stop retrying.

## With Sparrow

1. In Sparrow, register a webhook whose URL is your Testhooks endpoint, `https://your-testhooks/h/<slug>`. Copy the `webhook_secret` from the response; Sparrow shows it only once.
2. Paste the secret into the endpoint's Signature Verification settings.
3. Push an event in Sparrow. The delivery appears in Testhooks marked **signed**.

To check that a receiver rejects tampered requests, replay a delivery with one byte of the body changed, or with an old `webhook-timestamp`. With enforcement on, Testhooks answers `401`.

If Sparrow runs on the same machine as Testhooks, start Sparrow with `SPARROW_ALLOW_PRIVATE_NETWORKS=true` so it may deliver to a private address.

### How Testhooks answers Sparrow

Sparrow retries some failures and gives up on others. Testhooks answers so that this does the right thing:

| Testhooks answers | Sparrow |
|---|---|
| `2xx` | Success |
| `503` (database unavailable) | Retries with backoff |
| `429` (rate limited) | Retries after `Retry-After` |
| `404`, `401`, `413` | Permanent failure, no retry |

## Configure with the API

Signature settings live in the endpoint's `config`. `PATCH` replaces the whole `config` object, so send the existing settings along with the new ones:

```bash
curl -X PATCH http://localhost:8080/api/endpoints/<id> \
  -H 'Content-Type: application/json' \
  -d '{"config":{"forward_url":"https://example.com/hook","signature":{"secret":"whsec_...","enforce":true}}}'
```

| Field | |
|---|---|
| `signature.secret` | HMAC secret for `v1` signatures |
| `signature.public_key` | Hex Ed25519 public key for `v1a` signatures |
| `signature.enforce` | Reject failures with `401` |

Requests returned by the API include a `signature` object: `status` (`verified`, `invalid` or `missing`), `scheme`, `error` and `webhook_id`.

The verifier is tested against Sparrow's published signature test vectors.
