-- Fails if any slug is longer than 12 characters; rename or delete those
-- endpoints first.
ALTER TABLE endpoints ALTER COLUMN slug TYPE VARCHAR(12);
