-- Store when the person confirmed being 18 or older and when they accepted
-- which version of the terms, instead of a yes/no flag: consent needs a time.

ALTER TABLE users
    ADD COLUMN adult_confirmed_at timestamptz,
    ADD COLUMN terms_accepted_at  timestamptz,
    ADD COLUMN terms_version      text;

-- Keep any earlier confirmation, dated to when the account was created.
UPDATE users SET adult_confirmed_at = created_at WHERE is_adult_confirmed;

ALTER TABLE users DROP COLUMN is_adult_confirmed;

ALTER TABLE users
    ADD CONSTRAINT users_terms_version_with_time
        CHECK ((terms_accepted_at IS NULL) = (terms_version IS NULL));
