-- Email verification. A code is 5 digits (the design has 5 boxes), stored only
-- as an HMAC keyed with a server secret: a plain hash of a 5-digit code could be
-- reversed by trying all 100,000 values, the HMAC cannot without the secret.

ALTER TABLE users ADD COLUMN email_verified_at timestamptz;

CREATE TABLE email_codes (
    id          uuid        PRIMARY KEY DEFAULT uuidv7(),
    user_id     uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    purpose     text        NOT NULL,
    code_hash   bytea       NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    attempts    integer     NOT NULL DEFAULT 0,
    consumed_at timestamptz, -- used successfully, or replaced by a newer code

    CONSTRAINT email_codes_purpose_known CHECK (purpose IN ('verify_email', 'reset_password')),
    CONSTRAINT email_codes_hash_length CHECK (octet_length(code_hash) = 32),
    CONSTRAINT email_codes_attempts_not_negative CHECK (attempts >= 0)
);

-- At most one live code per person and purpose; also serves the hourly count.
CREATE UNIQUE INDEX email_codes_one_live_per_purpose ON email_codes (user_id, purpose) WHERE consumed_at IS NULL;
CREATE INDEX email_codes_user_purpose_created_idx ON email_codes (user_id, purpose, created_at);
