---
name: testhooks
description: Build or debug a webhook handler from real traffic with Testhooks. Use when the user wants to receive, inspect, test or replay webhooks (Stripe, GitHub, Svix, Sparrow, any provider), needs a webhook URL to paste into a provider, wants to write a handler from what a provider really sends, or wants to transform or reply to webhooks before they reach their app.
---

# Testhooks

Testhooks gives you webhook URLs that record every request sent to them. You get the URL, the user pastes it into the provider, and you read what the provider really sent instead of what its docs say it sends. Then you write the handler, preview any script, and send a test request to check it.

## Which tools you have

Prefer the `testhooks` MCP server when it is configured (tools `create_endpoint`, `wait_for_request`, `get_request`, `preview_script`, `send_request`, `replay_request`, `update_endpoint_config`, `list_endpoints`, `list_requests`, `get_endpoint`, `delete_endpoint`). Otherwise use the `testhooks` CLI (`testhooks endpoints …`, `testhooks requests …`, all with `--json`), or plain `curl` against the REST API, documented at `/api/openapi.json` on the server and at https://sarathsp06.github.io/testhooks/reference/api/.

Set `TESTHOOKS_URL` to the Testhooks to use (default `http://localhost:8080`; the live demo is `https://testhooks.vercel.app`) and `TESTHOOKS_AUTH_TOKEN` when the server has an `AUTH_TOKEN`. A provider on the internet can only reach capture URLs on a public Testhooks, not on localhost.

## The workflow

1. **Create an endpoint.** `create_endpoint` with a `slug_name` naming the provider (`stripe`, `github`) and a `name`. Give the user the `capture_url` and tell them to paste it into the provider's webhook settings and trigger an event. Say that a `test` or `ping` event from the provider's dashboard is enough.
2. **Wait, don't poll.** `wait_for_request` with the `endpoint_id` blocks until a request arrives (default 60 s, up to 300). When it returns `timed_out: true`, nothing has arrived: ask the user whether the URL is in place, then wait again. Pass the id of the last request you saw as `after` so you only get new ones.
3. **Read the request.** `get_request` gives method, path, query, headers, the body (and `body_json` when it is JSON), the signature check if configured, and a `curl` line that sends the same request. Note the event type field, the id and timestamp headers, the signature header and scheme, and the content type. These are what the handler has to accept.
4. **Write the handler** in the user's framework from that request. Verify the signature the way the provider does (Testhooks tells you whether it is Standard Webhooks, HMAC or Ed25519 when `signature` is configured). Return 2xx quickly and do the work after.
5. **Check it.** Point the endpoint at the user's handler (`update_endpoint_config` with `forward_url`, or browser mode when the handler runs on localhost) and `replay_request` the captured request, or `send_request` with your own body. Read `forward_result` and `response_info` on the request to see what the handler answered. Fix and repeat.

## Scripts: preview before saving

A transform (`wasm_script`, defining `transform(req)`) changes a request before it is forwarded; a reply script (`custom_response.script`, defining `handler(req)`) decides what the sender gets back. Always `preview_script` on a captured request before saving one with `update_endpoint_config`: the preview shows the request as it would be forwarded and the reply the sender would get, or the error the script threw. Nothing is sent or saved by a preview. Saving a reply script that throws means the sender gets the default reply, so a preview that reports `error` is not done.

## Defaults and safety

- A new endpoint only captures. Forwarding is off until `forward_url` is set; scripts don't run until saved; signature enforcement is off until `signature.enforce` is true.
- `update_endpoint_config` changes only the fields you pass. Set a field to `null` to remove it. The signature secret is write-only: omit it to keep the stored one.
- `delete_endpoint` removes the endpoint and every request it received and cannot be undone. Ask before deleting an endpoint you did not create in this session.
- Captured bodies may hold personal data. Quote only what the handler needs.
- When the server gives you a read-only token (`403` with "this token can only read"), you can list, read and wait but not create, change, delete or send. Tell the user what you need the full token for.

## Asking the user

Ask for: which provider and which events, where the handler runs (localhost or a public URL), and the framework. Do not ask for the provider's signing secret unless the user wants Testhooks itself to verify or re-sign; the handler can be tested with `send_request` without it.
