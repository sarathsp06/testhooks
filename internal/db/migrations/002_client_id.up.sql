-- Add client_id column for UX convenience (not access control).
-- Allows filtering endpoints by browser-generated UUID.
ALTER TABLE endpoints ADD COLUMN client_id VARCHAR(36);
CREATE INDEX idx_endpoints_client_id ON endpoints(client_id);
