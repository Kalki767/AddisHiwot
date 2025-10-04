-- A session is one logged-in device: a family of refresh tokens, each replacing
-- the one before it. Reusing a token that was already replaced means it was
-- copied, so the whole session is revoked.

CREATE TABLE auth_sessions (
    id             uuid        PRIMARY KEY DEFAULT uuidv7(),
    user_id        uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at     timestamptz NOT NULL DEFAULT now(),
    last_used_at   timestamptz NOT NULL DEFAULT now(),
    revoked_at     timestamptz,
    revoked_reason text,

    CONSTRAINT auth_sessions_revoked_with_reason CHECK ((revoked_at IS NULL) = (revoked_reason IS NULL)),
    CONSTRAINT auth_sessions_revoked_reason_known
        CHECK (revoked_reason IN ('logout', 'reuse_detected', 'password_reset', 'account_deleted'))
);

CREATE INDEX auth_sessions_user_id_idx ON auth_sessions (user_id);

-- Only a SHA-256 hash of each refresh token is stored: a database leak does not
-- give anyone a working token. The token itself has 256 random bits, so a fast
-- hash is enough (slow hashing is for guessable passwords).
CREATE TABLE refresh_tokens (
    id         uuid        PRIMARY KEY DEFAULT uuidv7(),
    session_id uuid        NOT NULL REFERENCES auth_sessions (id) ON DELETE CASCADE,
    token_hash bytea       NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    used_at    timestamptz, -- set when this token was exchanged for the next one

    CONSTRAINT refresh_tokens_token_hash_key UNIQUE (token_hash),
    CONSTRAINT refresh_tokens_token_hash_length CHECK (octet_length(token_hash) = 32)
);

CREATE INDEX refresh_tokens_session_id_idx ON refresh_tokens (session_id);
