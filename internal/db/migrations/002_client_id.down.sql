DROP INDEX IF EXISTS idx_endpoints_client_id;
ALTER TABLE endpoints DROP COLUMN IF EXISTS client_id;
