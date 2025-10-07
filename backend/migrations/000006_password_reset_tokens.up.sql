-- Resetting a password has two steps: the person proves they can read the email
-- (a 5-digit code, purpose 'reset_password' in email_codes), and then gets a
-- short-lived token that lets them set a new password once.
CREATE TABLE password_reset_tokens (
    id         uuid        PRIMARY KEY DEFAULT uuidv7(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- Only the SHA-256 of the token is stored, like refresh tokens.
    token_hash bytea       NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,

    CONSTRAINT password_reset_tokens_hash_key UNIQUE (token_hash),
    CONSTRAINT password_reset_tokens_hash_length CHECK (octet_length(token_hash) = 32)
);

CREATE INDEX password_reset_tokens_user_id_idx ON password_reset_tokens (user_id);
