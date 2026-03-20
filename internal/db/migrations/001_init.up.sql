-- 001_init.up.sql
-- Initial schema for Testhooks.

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Endpoints: each one is a unique webhook capture URL.
CREATE TABLE IF NOT EXISTS endpoints (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug        VARCHAR(12) UNIQUE NOT NULL,
    name        TEXT NOT NULL DEFAULT '',
    mode        VARCHAR(10) NOT NULL DEFAULT 'server',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    config      JSONB NOT NULL DEFAULT '{}'
);

-- Captured requests.
CREATE TABLE IF NOT EXISTS requests (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    endpoint_id  UUID NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    method       VARCHAR(10) NOT NULL,
    path         TEXT NOT NULL DEFAULT '/',
    headers      JSONB NOT NULL DEFAULT '{}',
    query        JSONB,
    body         BYTEA,
    content_type TEXT NOT NULL DEFAULT '',
    ip           TEXT NOT NULL DEFAULT '',
    size         INT NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_requests_endpoint
    ON requests (endpoint_id, created_at DESC);
