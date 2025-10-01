-- Accounts. One row per person; Google-only accounts have no password hash.

-- Case-insensitive text: 'Kidist@Mail.com' and 'kidist@mail.com' are the same
-- value, so the unique constraints below cannot be dodged by changing case.
CREATE EXTENSION IF NOT EXISTS citext;

-- Keeps updated_at current on every UPDATE; reused by later tables.
CREATE FUNCTION set_updated_at() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$;

CREATE TABLE users (
    -- UUIDv7: random but time-ordered, so new rows go to the end of the index.
    id                 uuid        PRIMARY KEY DEFAULT uuidv7(),
    email              citext      NOT NULL,
    username           citext      NOT NULL,
    -- NULL for accounts that only sign in with Google.
    password_hash      text,
    display_name       text        NOT NULL,
    locale             text        NOT NULL DEFAULT 'en',
    timezone           text        NOT NULL DEFAULT 'Africa/Addis_Ababa',
    is_adult_confirmed boolean     NOT NULL DEFAULT false,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT users_email_key    UNIQUE (email),
    CONSTRAINT users_username_key UNIQUE (username),
    CONSTRAINT users_email_format CHECK (length(email) <= 254 AND email ~ '^[^@\s]+@[^@\s]+$'),
    -- 3 to 20 characters, starts with a letter, then letters, digits, '_' or '.';
    -- never '@', so a login identifier with '@' is always an email.
    CONSTRAINT users_username_format CHECK (username ~* '^[a-z][a-z0-9_.]{2,19}$'),
    CONSTRAINT users_display_name_length CHECK (char_length(display_name) BETWEEN 1 AND 50),
    CONSTRAINT users_locale_supported CHECK (locale IN ('en', 'am'))
);

CREATE TRIGGER users_set_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
