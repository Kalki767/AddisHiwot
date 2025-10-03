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
