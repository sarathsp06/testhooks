---
title: Webhooks to localhost
description: Use browser mode as a webhook tunnel to your local app, with nothing to install.
---

Testing a webhook integration usually means installing a tunnel such as ngrok, running a daemon and keeping a terminal open. Browser mode does the same job from a browser tab.

```
Stripe / GitHub / Slack  ──►  Testhooks /h/:slug  ──WebSocket──►  your browser tab  ──fetch()──►  localhost:3000
```

1. The provider sends the webhook to your public Testhooks URL.
2. Testhooks relays it to your open tab over WebSocket.
3. Your tab forwards it to your local app with `fetch()`.

Because the last hop starts in your browser, it reaches anything your machine can: `localhost`, Docker containers, hosts on your VPN.

## Set it up

1. Create an endpoint and switch it to **Browser** in the **Settings** tab.
2. Under **Forwarding**, set the target URL to your local app, for example `http://localhost:3000/webhooks/stripe`, and save.
3. Paste the endpoint's URL (`https://your-testhooks/h/<slug>`) into the provider's webhook settings.
4. Keep the tab open while you test.

Each delivery shows up in the list, then lands on your local app. Optionally add a [transform](../scripts/) to reshape it on the way.

## Make your local app accept the request

The request comes from your browser, so it is a cross-origin `fetch()`. Your local app has to allow it:

- Answer CORS preflight (`OPTIONS`) requests and send `Access-Control-Allow-Origin` with your Testhooks origin, or `*` in development.
- Allow the headers being forwarded, such as `Content-Type` and any signature headers your app checks.

If forwarding fails with a network error in the browser console, CORS is the usual cause.

## Compared to a tunnel

| | ngrok / localtunnel | Testhooks browser mode |
|---|---|---|
| Setup | Install a CLI, authenticate, run a daemon | Open a browser tab |
| Inspecting payloads | Separate dashboard | Built-in live view |
| Transform before delivery | Not possible | JavaScript, Lua or Jsonnet |
| Custom responses | Whatever your local app returns | Scripted, to test error handling without changing your app |
| Data | Routed through a third-party service | Relayed by your own server, never stored |

The trade-off: forwarding only happens while the tab is open. For always-on forwarding to public services, use [server mode](../modes/).
