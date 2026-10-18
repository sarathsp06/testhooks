---
title: Installation
description: One-line Docker and binary installs, with PostgreSQL or in-memory storage.
---

Testhooks is a single Go binary with the web UI embedded. It stores data in PostgreSQL 15 or newer, or in memory for local testing.

## Install the binary

The quickest start: no Docker and, with in-memory storage, no database. Install once; it downloads the build for your OS and architecture and verifies its SHA-256 checksum:

```bash
curl -fsSL https://raw.githubusercontent.com/sarathsp06/testhooks/main/install.sh | sh
```

Then run it, now and every time after:

```bash
DATABASE_URL=memory testhooks
```

Installer options:

| Variable | Default | |
|---|---|---|
| `TESTHOOKS_VERSION` | `latest` | Release tag to install, for example `v1.1.0` |
| `TESTHOOKS_INSTALL_DIR` | `/usr/local/bin` if writable, else `~/.local/bin` | Where to put the binary |

If it lands in `~/.local/bin` and that isn't on your `PATH`, run `~/.local/bin/testhooks` (the installer prints a hint). Builds are published for Linux, macOS and Windows on amd64 and arm64. You can also download them directly from the [releases page](https://github.com/sarathsp06/testhooks/releases).

### Run without a database

For local testing, keep everything in memory:

```bash
DATABASE_URL=memory testhooks
```

Data is lost when the process stops. To keep it across restarts, add a snapshot file. The store is written to it every 30 seconds when something changed, and on shutdown, then loaded on the next start:

```bash
DATABASE_URL=memory SNAPSHOT_PATH=./testhooks.snapshot testhooks
```

A clean shutdown loses nothing; a crash loses at most the last 30 seconds. If the snapshot file is unreadable, Testhooks refuses to start rather than overwrite it.

In-memory mode is meant for one person testing locally. For a shared or public instance, use PostgreSQL.

:::caution[Needs one long-running process]
In-memory data lives in a single process. On platforms that spread requests across several short-lived instances, such as Vercel, Netlify Functions or AWS Lambda, each instance has its own memory, so endpoints created on one instance are missing on the next and webhook URLs return 404. A snapshot file doesn't help there either, because each instance has its own temporary disk.

Run in-memory Testhooks as one long-running process: your machine, a VPS, or a container host (Fly.io, Railway, Render and similar) with a single instance. On Vercel, use PostgreSQL; see [Deploy on Vercel](../../deployment/vercel/).
:::

### With PostgreSQL

For a quick local database:

```bash
docker run -d --name testhooks-pg -p 127.0.0.1:5432:5432 -e POSTGRES_USER=testhooks -e POSTGRES_PASSWORD=testhooks -e POSTGRES_DB=testhooks postgres:17-alpine
DATABASE_URL='postgres://testhooks:testhooks@localhost:5432/testhooks?sslmode=disable' testhooks
```

Point it at another database with `DATABASE_URL`. See [Configuration](../../reference/configuration/) for every setting.

## Docker, no database

For local testing. One container, data kept in memory:

```bash
docker run -d --name testhooks -p 8080:8080 -e DATABASE_URL=memory ghcr.io/sarathsp06/testhooks
```

To keep the data across restarts, add a volume and a [snapshot file](#run-without-a-database):

```bash
docker run -d --name testhooks -p 8080:8080 -v testhooks-data:/data -e DATABASE_URL=memory -e SNAPSHOT_PATH=/data/testhooks.snapshot ghcr.io/sarathsp06/testhooks
```

Remove it with `docker rm -f testhooks` (and `docker volume rm testhooks-data` for the snapshot).

## Docker Compose with PostgreSQL

For shared or long-running instances. Runs Testhooks and PostgreSQL together from the published image; migrations run on startup and the server waits up to 60 seconds for the database:

```bash
curl -fsSL https://raw.githubusercontent.com/sarathsp06/testhooks/main/deploy/docker-compose.yml | docker compose -p testhooks -f - up -d
```

Set options as environment variables in front of the command:

| Variable | Default | |
|---|---|---|
| `TESTHOOKS_PORT` | `8080` | Host port to publish |
| `TESTHOOKS_VERSION` | `latest` | Image tag, for example `1.1.0` |
| `AUTH_TOKEN` | | Require a bearer token for the UI and API |
| `ALLOWED_ORIGINS` | `*` | Allowed browser origins |
| `TRUSTED_PROXIES` | | CIDRs of reverse proxies allowed to set client-IP headers |

```bash
TESTHOOKS_PORT=9000 AUTH_TOKEN=change-me \
  curl -fsSL https://raw.githubusercontent.com/sarathsp06/testhooks/main/deploy/docker-compose.yml | docker compose -p testhooks -f - up -d
```

To keep a local copy you can edit, download it first:

```bash
curl -fsSLO https://raw.githubusercontent.com/sarathsp06/testhooks/main/deploy/docker-compose.yml
docker compose up -d
```

## Container image

Multi-arch images (amd64, arm64) are published to the GitHub Container Registry:

```bash
docker run -d -p 8080:8080 -e DATABASE_URL='postgres://user:pass@db-host:5432/testhooks?sslmode=disable' ghcr.io/sarathsp06/testhooks
```

Use `DATABASE_URL=memory` instead to run [without a database](#docker-no-database).

The image is built `FROM scratch`: it holds only the binary, CA certificates and time zone data.

## From source

Prerequisites: Go 1.26+ and Node 22+. PostgreSQL is optional: without `DATABASE_URL`, `./testhooks` stores data in memory.

```bash
git clone https://github.com/sarathsp06/testhooks.git
cd testhooks

docker compose up --build   # app + Postgres built from this checkout
# or
make build                  # builds the UI, then the binary with the UI embedded
./testhooks
```

`make build-all` cross-compiles for every supported OS and architecture into `dist/`.

### Development

```bash
make dev        # Go on :8080 (API, WebSocket, proxy to Vite) and Vite on :5173 with hot reload
make test       # Go tests
make lint       # go vet and svelte-check
```
