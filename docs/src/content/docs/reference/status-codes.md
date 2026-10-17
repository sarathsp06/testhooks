---
title: Status codes
description: What a webhook sender gets back from a capture URL.
---

| Status | When | Sparrow-style senders |
|---|---|---|
| `200 OK` | Captured in server mode with the default response | Success |
| `202 Accepted` | Captured in browser mode, or a browser-mode response handler didn't answer in 10 s | Success |
| *your status* | A [response handler](../../guides/scripts/#custom-responses) or a sync [forward](../../guides/forwarding/#async-and-sync) target set it | Depends on the status |
| `400 Bad Request` | The slug has invalid characters | No retry |
| `401 Unauthorized` | [Signature enforcement](../../guides/signatures/#reject-bad-signatures) is on and the signature is missing or invalid | No retry |
| `404 Not Found` | No endpoint has that slug | No retry |
| `413 Payload Too Large` | The body exceeds `MAX_BODY_SIZE` | No retry |
| `429 Too Many Requests` | The client IP is over the rate limit; includes `Retry-After: 1` | Retries later |
| `502 Bad Gateway` | A sync forward got no response from the target | Retries |
| `503 Service Unavailable` | The database is unreachable | Retries |

The default response body is:

```json
{"status":"ok","id":"<request id>"}
```

Retry behaviour varies by sender. Many, including Sparrow, retry `5xx` and `429` but treat other `4xx` responses as permanent failures, which is why a database outage returns `503` rather than `404`.
