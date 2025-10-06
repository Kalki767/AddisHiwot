DROP TABLE IF EXISTS email_codes;
ALTER TABLE users DROP COLUMN IF EXISTS email_verified_at;
