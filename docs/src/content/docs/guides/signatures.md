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

## How secrets are stored

The HMAC secret is write-only. Once saved, the API and the UI only say that a secret is set (`"secret_set": true`); paste a new one to replace it, or choose **Remove**. The Ed25519 public key isn't secret and is shown as saved.

Secrets are stored with envelope encryption, the same scheme [Sparrow](https://github.com/sarathsp06/sparrow) uses. Each secret is encrypted with its own random data key (AES-256-GCM), and that data key is encrypted with a key encryption key (KEK) from `ENCRYPTION_KEYS`. The database or snapshot file alone isn't enough to read a secret.

Set a key before saving secrets on a persistent server:

```bash
ENCRYPTION_KEYS=k1=$(openssl rand -hex 32)
```

Keep the key somewhere safe. Without it, stored secrets can't be decrypted, and endpoints that enforce signatures reject every request until you paste the secrets again.

### Rotate the key

1. Add a new key and make it primary, keeping the old one:

   ```bash
   ENCRYPTION_KEYS=k1=<old key>,k2=<new key>
   ENCRYPTION_PRIMARY_KEY_ID=k2
   ```

2. Restart. On startup Testhooks rewraps every secret's data key with `k2`, and logs how many it changed. The secrets themselves aren't re-encrypted.
3. Once every instance runs with the new settings, remove `k1` from `ENCRYPTION_KEYS`.

Secrets saved before encryption existed are encrypted the same way the first time Testhooks starts with a key.

## Reject bad signatures

Turn on **Reject invalid signatures with 401** to make the endpoint answer like a real receiver: requests with a missing or invalid signature get `401 Unauthorized`.

```json
{ "error": "invalid webhook signature", "detail": "v1 signature does not match" }
```

Rejected requests are still captured, so you can see what was sent. Senders such as Sparrow treat `401` as permanent and stop retrying.

## Re-sign after a transform

A [transform](../scripts/) that changes the body breaks the sender's signature: the signature covers the body as it was sent, so the app you forward to rejects the changed one. Turn on **Sign what I forward with this secret** (under the secret field) and Testhooks signs the forwarded request again with the same secret, after the transform:

- `webhook-signature` becomes a fresh `v1` signature of the forwarded body. Other signatures, such as `v1a`, are dropped, since they would no longer verify.
- `webhook-timestamp` becomes the time of the forward.
- `webhook-id` stays the sender's, so your app can still spot retries. A request without one gets `msg_<request id>`.

Your app keeps checking signatures with the secret it already has, and sees the transformed body.

Only a request whose `v1` signature verified with this secret is re-signed. Anything else (unsigned, a bad signature, or one that only verified with the Ed25519 public key) is forwarded with its headers as they were, and your app rejects it as it should. So knowing your webhook URL is not enough to get a body signed. The request shows **re-signed** or **not re-signed** next to the forward result, with the reason under **What was forwarded**.

This works in both modes. In browser mode the tab forwards, but the secret never leaves the server: the tab sends the request as captured and the body it is about to forward to [`POST /api/endpoints/{id}/resign`](../../reference/api/#re-sign-a-forwarded-body), which checks the captured request's signature and returns the new headers.

The signer is checked against the signatures in Sparrow's test vectors, so a receiver that verifies Sparrow deliveries verifies re-signed ones too.

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
| `signature.resign` | [Re-sign forwarded requests](#re-sign-after-a-transform) with the secret. Needs `signature.secret` |

Requests returned by the API include a `signature` object: `status` (`verified`, `invalid` or `missing`), `scheme`, `error` and `webhook_id`.

The verifier is tested against Sparrow's published signature test vectors.
