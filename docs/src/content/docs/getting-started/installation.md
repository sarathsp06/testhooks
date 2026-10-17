---
title: Installation
description: Docker Compose, the binary installer, the container image, or building from source.
---

Testhooks is a single Go binary with the web UI embedded. Its only dependency is PostgreSQL 15 or newer. Database migrations run automatically on startup, and the server waits up to 60 seconds for the database to become reachable.

## Docker Compose

Runs Testhooks and PostgreSQL together from the published image. Nothing to clone or build:

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

## Install the binary

Downloads the build for your OS and architecture from GitHub Releases and verifies its SHA-256 checksum:

```bash
curl -fsSL https://raw.githubusercontent.com/sarathsp06/testhooks/main/install.sh | sh
```

| Variable | Default | |
|---|---|---|
| `TESTHOOKS_VERSION` | `latest` | Release tag to install, for example `v1.1.0` |
| `TESTHOOKS_INSTALL_DIR` | `/usr/local/bin` if writable, else `~/.local/bin` | Where to put the binary |

Builds are published for Linux, macOS and Windows on amd64 and arm64. You can also download them directly from the [releases page](https://github.com/sarathsp06/testhooks/releases).

The binary needs PostgreSQL. For a quick local database:

```bash
docker run -d --name testhooks-pg -p 127.0.0.1:5432:5432 \
  -e POSTGRES_USER=testhooks -e POSTGRES_PASSWORD=testhooks -e POSTGRES_DB=testhooks \
  postgres:17-alpine

testhooks             # connects to postgres://testhooks:testhooks@localhost:5432/testhooks by default
testhooks --version
```

Point it at another database with `DATABASE_URL`. See [Configuration](../../reference/configuration/) for every setting.

## Container image

Multi-arch images (amd64, arm64) are published to the GitHub Container Registry:

```bash
docker run -d -p 8080:8080 \
  -e DATABASE_URL='postgres://user:pass@db-host:5432/testhooks?sslmode=disable' \
  ghcr.io/sarathsp06/testhooks:latest
```

The image is built `FROM scratch`: it holds only the binary, CA certificates and time zone data.

## From source

Prerequisites: Go 1.26+, Node 22+ and PostgreSQL.

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
