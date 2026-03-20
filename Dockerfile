# ---- Stage 1: Build SPA ----
FROM node:22-alpine AS web
WORKDIR /app/web
COPY web/package.json web/package-lock.json* ./
RUN npm ci
COPY web/ ./
RUN npm run build

# ---- Stage 2: Build Go binary ----
FROM golang:1.24-alpine AS build
RUN apk add --no-cache tzdata
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Replace placeholder dist/ with real SPA build from stage 1
COPY --from=web /app/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /testhooks ./cmd/testhooks

# ---- Stage 3: Runtime (scratch — no OS, just the binary) ----
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /testhooks /testhooks
EXPOSE 8080
ENTRYPOINT ["/testhooks"]
