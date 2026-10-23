---
title: Webhooks to localhost
description: Use browser mode as a webhook tunnel to your local app, with nothing to install. Your app's reply goes back to the sender.
---

Testing a webhook handler usually means installing a tunnel such as ngrok, authenticating, running a daemon and keeping a terminal open. Browser mode does the same job from a browser tab, and goes one step further than a request bin: the reply your local app gives is the reply the sender gets.

```
Stripe / GitHub / Slack  ──►  Testhooks /h/:slug  ──WebSocket──►  your browser tab  ──fetch()──►  localhost:3000
                         ◄──  201 Created        ◄──────────────  the tab            ◄─────────  201 Created
```

1. The provider sends the webhook to your public Testhooks URL. Testhooks holds that request open.
2. Testhooks relays it to your open tab over WebSocket.
3. Your tab forwards it to your local app with `fetch()` and waits for the reply.
4. The tab sends your app's status, headers and body back, and Testhooks returns them to the provider.

Because the hop to your app starts in your browser, it reaches anything your machine can: `localhost`, Docker containers, hosts on your VPN. Because the reply travels back the same way, the provider sees your handler's real answer: a `200` marks the delivery successful, a `500` makes it retry, exactly as in production.

## Set it up

1. Create an endpoint and choose **In this browser tab**, or switch an existing one to **Browser** in its **Settings** tab.
2. Under **Forwarding**, set the target URL to your local app, for example `http://localhost:3000/webhooks/stripe`.
3. Turn on **Reply to the sender with my app's response** and save.
4. Paste the endpoint's URL (`https://your-testhooks/h/<slug>`) into the provider's webhook settings.
5. Keep the tab open while you test.

Each delivery shows up in the list, lands on your local app, and the request's **Reply** shows what went back to the provider, marked as coming from your app. Optionally add a [transform](../scripts/) to reshape the payload on the way, or a [reply script](../scripts/#custom-responses) that sees your app's reply as `req.forward_response` and decides what the sender gets.

With the switch off, forwarding is fire-and-forget: the provider gets `202 Accepted` at once and your app's reply is only shown in the UI.

### Timing and fallbacks

- The provider's request is held for up to 10 seconds. If your app hasn't replied by then, or no tab is open, the provider gets `202 Accepted`.
- If the forward fails outright (connection refused, CORS blocked, timeout), the provider gets `502 Bad Gateway` with the reason in the body, the same as a failed sync forward in server mode.
- A request a transform skips (returns `null`) gets the default reply and is not forwarded.

## Make your local app accept the request

The request comes from your browser, so it is a cross-origin `fetch()`. Two things have to allow it.

**CORS.** Your app has to answer the preflight `OPTIONS` request and send `Access-Control-Allow-Origin` with your Testhooks origin, or `*` in development, and `Access-Control-Allow-Headers` covering the headers being forwarded, such as `Content-Type` and any signature headers your handler checks. If forwarding fails with a network error and the request page shows a `502` reply, CORS is the usual cause; the browser console has the exact header it rejected.

Development-only examples:

```js
// Express
app.use((req, res, next) => {
  res.set('Access-Control-Allow-Origin', '*');
  res.set('Access-Control-Allow-Headers', '*');
  if (req.method === 'OPTIONS') return res.sendStatus(204);
  next();
});
```

```go
// Go net/http
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		if r.Method == http.MethodOptions { w.WriteHeader(http.StatusNoContent); return }
		next.ServeHTTP(w, r)
	})
}
```

```python
# FastAPI
from fastapi.middleware.cors import CORSMiddleware
app.add_middleware(CORSMiddleware, allow_origins=["*"], allow_methods=["*"], allow_headers=["*"])
```

Your app's status, body and `Content-Type` always come back to the sender. Any other response header reaches the sender only if your app lists it in `Access-Control-Expose-Headers`, because the browser hides the rest from the tab.

Scope these to development. Your production webhook handler is called by the provider's servers, not a browser, and needs no CORS at all.

**Local network access.** Since Chrome 142, a page on a public origin that calls `localhost` or a private address triggers a one-time permission prompt. Allow it for your Testhooks origin. If you dismissed it, click the icon in the address bar to grant it; a denied prompt looks like a CORS failure.

## Compared to a tunnel

| | ngrok / localtunnel | Testhooks browser mode |
|---|---|---|
| Setup | Install a CLI, authenticate, run a daemon | Open a browser tab |
| Your app's reply reaches the sender | Yes | Yes, with **Reply to the sender with my app's response** |
| Inspecting payloads | Separate dashboard, often a paid tier | Built-in live view, headers, body, signature check |
| Transform before delivery | Not possible | JavaScript, with a live diff |
| Custom responses | Whatever your local app returns | Your app's reply, or a script that decides |
| Signature verification | Metered add-on, or none | Standard Webhooks, inline on every request |
| Data | Routed through a third-party service | Relayed by your own server, never stored |
| Stable URL | Random per session on free tiers | Your endpoint's URL, for as long as you keep it |

The trade-off: forwarding and the local reply only happen while the tab is open. For always-on forwarding to public services, use [server mode](../modes/).
