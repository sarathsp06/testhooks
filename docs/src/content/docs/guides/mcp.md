---
title: Use it from an AI agent (MCP)
description: Let Claude Code, Cursor or another MCP client create endpoints and read the webhooks that arrive, so it can write your handler from real traffic.
---

`testhooks mcp` is a small [MCP](https://modelcontextprotocol.io/) server that lets an AI coding agent use Testhooks the way you use the UI. It gives you a better handler, written from what the provider really sends instead of what its docs say it sends.

A typical session:

1. You ask the agent to "set up a webhook endpoint for Stripe". It creates one and gives you the URL.
2. You paste that URL into the provider's webhook settings and trigger an event.
3. The agent reads the requests that arrived (headers, body, signature) and writes the handler to match.
4. It replays a captured request to check the handler, as if the provider sent it again.

The MCP server runs on your machine and talks to a running Testhooks over the [REST API](../../reference/api/). It adds nothing of its own: every tool is an API call you could make with `curl`.

## Set it up

You need the `testhooks` binary on your machine (see [Installation](../../getting-started/installation/)) and a Testhooks to talk to: your own (`testhooks` in another terminal), a deployed one, or the [live demo](https://testhooks.vercel.app/).

| Variable | | |
|---|---|---|
| `TESTHOOKS_URL` | optional | Address of the Testhooks to use. Defaults to `http://localhost:8080` |
| `TESTHOOKS_AUTH_TOKEN` | optional | The server's [`AUTH_TOKEN`](../../reference/configuration/#security), when it has one. Sent as `Authorization: Bearer <token>` |

The capture URLs the agent hands out start with `TESTHOOKS_URL`, so a provider on the internet can only reach them when that address is public.

### Claude Code

```bash
claude mcp add testhooks -e TESTHOOKS_URL=http://localhost:8080 -- testhooks mcp
```

Add `-e TESTHOOKS_AUTH_TOKEN=<token>` when the server needs one.

### Cursor and Claude Desktop

Add this to `.cursor/mcp.json` (Cursor) or `claude_desktop_config.json` (Claude Desktop):

```json
{
  "mcpServers": {
    "testhooks": {
      "command": "testhooks",
      "args": ["mcp"],
      "env": {
        "TESTHOOKS_URL": "http://localhost:8080",
        "TESTHOOKS_AUTH_TOKEN": ""
      }
    }
  }
}
```

If the app can't find `testhooks`, use its full path, for example `/usr/local/bin/testhooks`.

## Tools

| Tool | What it does |
|---|---|
| `create_endpoint` | Creates a webhook URL that records every request sent to it. Takes an optional `name`, `mode` (`server` or `browser`) and [`config`](../../reference/api/#endpoint-config). Returns the endpoint with its `capture_url` |
| `list_endpoints` | Lists endpoints, each with its `capture_url` |
| `get_endpoint` | Shows one endpoint by `id`, or by the `slug` in its capture URL |
| `update_endpoint_config` | Changes only the settings you pass and keeps the rest. Set a field to `null` to remove it |
| `list_requests` | Lists the requests an endpoint received, newest first |
| `get_request` | Shows one request in full: method, path, query, headers, body, signature check, what was forwarded and what the sender got back |
| `replay_request` | Sends a captured request to its endpoint again with the same method, path, query, headers and body, and returns the reply. Connection headers such as `Host` and `Content-Length` are not copied |
| `delete_endpoint` | Deletes an endpoint and all its requests. This cannot be undone; it is marked destructive, and most MCP clients ask before running such tools |

A replayed request is a new request: it shows up in the list, runs through the endpoint's transform, forwarding and reply like any other, and the agent sees the status the provider would have seen.

Signature secrets stay write-only here too: `update_endpoint_config` keeps the stored secret unless you pass a new `signature.secret`.
