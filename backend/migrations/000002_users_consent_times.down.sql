ALTER TABLE users ADD COLUMN is_adult_confirmed boolean NOT NULL DEFAULT false;
UPDATE users SET is_adult_confirmed = adult_confirmed_at IS NOT NULL;
ALTER TABLE users
    DROP CONSTRAINT users_terms_version_with_time,
    DROP COLUMN terms_version,
    DROP COLUMN terms_accepted_at,
    DROP COLUMN adult_confirmed_at;
