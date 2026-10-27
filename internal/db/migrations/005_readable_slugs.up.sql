-- Readable slugs: an optional name plus three words ("stripe-acid-fable-gully"),
-- up to 38 characters today. 64 leaves room without inviting long URLs.
ALTER TABLE endpoints ALTER COLUMN slug TYPE VARCHAR(64);
