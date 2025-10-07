-- name: CreateUser :one
INSERT INTO users (email, username, password_hash, display_name, locale, timezone,
                   adult_confirmed_at, terms_accepted_at, terms_version)
VALUES (@email, @username, @password_hash, @display_name, @locale, @timezone,
        @adult_confirmed_at, @terms_accepted_at, @terms_version)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = @id;

-- name: GetUserByEmail :one
-- email is citext, so this match ignores case.
SELECT * FROM users WHERE email = @email;

-- name: GetUserByUsername :one
-- username is citext, so this match ignores case.
SELECT * FROM users WHERE username = @username;

-- name: MarkEmailVerified :exec
-- Keeps the first verification time if it is called again.
UPDATE users SET email_verified_at = coalesce(email_verified_at, @verified_at) WHERE id = @id;

-- name: UpdatePasswordHash :exec
UPDATE users SET password_hash = @password_hash WHERE id = @id;
