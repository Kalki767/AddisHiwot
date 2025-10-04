-- name: CreateSession :one
INSERT INTO auth_sessions (user_id) VALUES (@user_id) RETURNING *;

-- name: CreateRefreshToken :exec
INSERT INTO refresh_tokens (session_id, token_hash, expires_at)
VALUES (@session_id, @token_hash, @expires_at);

-- name: GetRefreshToken :one
-- A token with its session, found by the token's hash.
SELECT t.id, t.session_id, t.expires_at, t.used_at, s.user_id, s.revoked_at
FROM refresh_tokens t
JOIN auth_sessions s ON s.id = t.session_id
WHERE t.token_hash = @token_hash;

-- name: MarkRefreshTokenUsed :execrows
-- Succeeds (1 row) only for the first caller: two requests racing with the same
-- token cannot both rotate it.
UPDATE refresh_tokens SET used_at = now() WHERE id = @id AND used_at IS NULL;

-- name: TouchSession :exec
UPDATE auth_sessions SET last_used_at = now() WHERE id = @id;

-- name: RevokeSession :execrows
UPDATE auth_sessions SET revoked_at = now(), revoked_reason = @reason
WHERE id = @id AND revoked_at IS NULL;
