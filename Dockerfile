# ---- Stage 1: Build SPA (platform-independent, so run on the build host) ----
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /app/web
COPY web/package.json web/package-lock.json* ./
RUN npm ci
COPY web/ ./
RUN npm run build

# ---- Stage 2: Build Go binary (cross-compiled for the target platform) ----
# Keep in sync with the go directive in go.mod: official Go images set
# GOTOOLCHAIN=local, so an older image cannot build the module.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
RUN apk add --no-cache tzdata ca-certificates
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Replace placeholder dist/ with real SPA build from stage 1
COPY --from=web /app/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=$VERSION" -o /testhooks ./cmd/testhooks

# ---- Stage 3: Runtime (scratch — no OS, just the binary) ----
FROM scratch
ARG VERSION=dev
LABEL org.opencontainers.image.title="testhooks" \
      org.opencontainers.image.description="Self-hostable webhook inspector, forwarder and mock server" \
      org.opencontainers.image.source="https://github.com/sarathsp06/testhooks" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="$VERSION"
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /testhooks /testhooks
EXPOSE 8080
ENTRYPOINT ["/testhooks"]
