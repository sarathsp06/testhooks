-- People who clicked "I'd pay for that" under the free-tier limits. One row
-- per browser (client_id), so the count is people, not clicks. endpoint_id
-- is where they were when they clicked; it is not a foreign key, so the row
-- outlives the endpoint.
CREATE TABLE IF NOT EXISTS upgrade_interest (
    client_id   TEXT PRIMARY KEY,
    endpoint_id TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
