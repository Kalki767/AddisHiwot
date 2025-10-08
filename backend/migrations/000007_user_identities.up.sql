-- Sign-in providers linked to an account. Only Google for now. The provider's
-- subject ("sub") identifies the person, never their email: an email can
-- change hands, the subject cannot.
CREATE TABLE user_identities (
    id         uuid        PRIMARY KEY DEFAULT uuidv7(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider   text        NOT NULL,
    subject    text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),

    -- One account per provider account, and one provider account per account.
    CONSTRAINT user_identities_provider_subject_key UNIQUE (provider, subject),
    CONSTRAINT user_identities_user_provider_key UNIQUE (user_id, provider),
    CONSTRAINT user_identities_provider_known CHECK (provider IN ('google')),
    -- Google's subject is at most 255 ASCII characters.
    CONSTRAINT user_identities_subject_length CHECK (length(subject) BETWEEN 1 AND 255)
);

-- Linking Google to an account whose email was never verified ends its sessions.
ALTER TABLE auth_sessions DROP CONSTRAINT auth_sessions_revoked_reason_known;
ALTER TABLE auth_sessions ADD CONSTRAINT auth_sessions_revoked_reason_known
    CHECK (revoked_reason IN ('logout', 'reuse_detected', 'password_reset', 'account_deleted', 'google_linked'));
