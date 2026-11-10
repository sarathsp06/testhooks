---
title: Command line
description: "Everything the UI does, from a shell: endpoints, requests, waiting, sending, previewing scripts."
---

The `testhooks` binary that runs the server is also a client for one. Each command is a call to the [REST API](../api/), the same calls the UI and [`testhooks mcp`](../../guides/mcp/) make, so they say and do the same things.

```bash
testhooks endpoints create --slug stripe
testhooks requests wait <endpoint>
testhooks requests get <request id>
```

## Which Testhooks, which token

| | |
|---|---|
| `--url`, or `TESTHOOKS_URL` | The Testhooks to talk to. Default `http://localhost:8080` |
| `--token`, or `TESTHOOKS_AUTH_TOKEN` | Its [`AUTH_TOKEN`](../configuration/#security), or its [`READ_TOKEN`](../../deployment/production/#a-read-only-token) to read and wait but not change anything |

An endpoint can be named by its `id` or by the slug in its capture URL (`abc-fable-gully` for `/h/abc-fable-gully`).

## Output and exit codes

When stdout is a terminal the output is short text with the next step; when it is a pipe (a script, an agent) it is the API's JSON, indented. `--json` and `--plain` force either.

| Exit code | |
|---|---|
| `0` | Done |
| `1` | The server said no, or couldn't be reached; the reason is on stderr |
| `2` | Wrong usage; the message says what to pass |
| `3` | `requests wait`: nothing arrived in time |

## Endpoints

| Command | |
|---|---|
| `endpoints list [--limit N]` | Endpoints, newest first, each with its capture URL |
| `endpoints create [--name S] [--slug S] [--mode server\|browser] [--config JSON]` | A new endpoint. It only captures until settings say otherwise |
| `endpoints get <id\|slug>` | One endpoint with its settings |
| `endpoints update <id\|slug> [--name S] [--slug S] [--mode M] [--config JSON]` | Change it. `--config` changes only the settings given and keeps the rest; a `null` removes one. `--slug` gives it a new URL and the old one stops working |
| `endpoints delete <id\|slug> [--yes]` | Delete it and every request it received. Asks first in a terminal; in a pipe, `--yes` is required |

## Requests

| Command | |
|---|---|
| `requests list <endpoint> [--limit N] [--after ID]` | Requests, newest first. `--after` lists only those newer than a request |
| `requests get <id>` | One request in full, with a `curl` line that sends it again |
| `requests wait <endpoint> [--after ID] [--timeout SECONDS]` | Wait until a request arrives and print it, instead of polling. Default 60 seconds, up to 300. Pass the last id you saw as `--after` to wait only for newer ones. Exits `3` when nothing came |
| `requests send <endpoint> [-X METHOD] [-p PATH] [-H 'Name: value']... [-d BODY\|@FILE\|-] [--content-type TYPE]` | Send a test request to the capture URL, as a provider would, and print what the sender got and what was captured. A JSON body gets `application/json` by default |
| `requests replay <id>` | Send a captured request to its endpoint again |
| `requests preview <endpoint> [--request ID] [--transform FILE] [--reply FILE]` | Run scripts on a captured request (the newest, or `--request`) without saving or sending anything, and print what would be forwarded and what the sender would get. `FILE` may be `-` for stdin; an empty value runs no such script; a flag left out runs the saved one |
| `requests delete <id>` | Delete one request |

A loop that waits for each request as it comes:

```bash
last=
while req=$(testhooks requests wait "$ep" --after "$last" --timeout 60 || [ $? = 3 ]); do
  [ -n "$req" ] && last=$(echo "$req" | jq -r '.requests[0].id') && echo "$req" | jq '.requests[0].body_json'
done
```

## For agents

| Command | |
|---|---|
| `mcp` | The [MCP server](../../guides/mcp/) on stdin and stdout |
| `skill` | Prints [`SKILL.md`](https://github.com/sarathsp06/testhooks/blob/main/skills/testhooks/SKILL.md), which teaches an agent the workflow. `testhooks skill > .claude/skills/testhooks/SKILL.md` |

`version` prints the version; `testhooks` alone runs the server.
