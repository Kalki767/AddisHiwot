-- name: CreateSession :one
INSERT INTO auth_sessions (user_id) VALUES (@user_id) RETURNING *;

-- name: CreateRefreshToken :one
INSERT INTO refresh_tokens (session_id, token_hash, expires_at)
VALUES (@session_id, @token_hash, @expires_at)
RETURNING id;

-- name: GetRefreshToken :one
-- A token with its session and whether the token that replaced it was used.
SELECT t.id, t.session_id, t.expires_at, t.used_at, t.replaced_by,
       (n.used_at IS NOT NULL)::boolean AS successor_used,
       s.user_id, s.revoked_at
FROM refresh_tokens t
JOIN auth_sessions s ON s.id = t.session_id
LEFT JOIN refresh_tokens n ON n.id = t.replaced_by
WHERE t.token_hash = @token_hash;

-- name: MarkRefreshTokenUsed :execrows
-- Succeeds (1 row) only for the first caller: two requests racing with the same
-- token cannot both rotate it. replaced_by is NULL for a token superseded by a
-- retry: it was never delivered, so presenting it can only mean a copy.
UPDATE refresh_tokens SET used_at = @used_at, replaced_by = @replaced_by
WHERE id = @id AND used_at IS NULL;

-- name: SetRefreshTokenReplacedBy :exec
UPDATE refresh_tokens SET replaced_by = @replaced_by WHERE id = @id;

-- name: TouchSession :exec
UPDATE auth_sessions SET last_used_at = now() WHERE id = @id;

-- name: RevokeSession :execrows
UPDATE auth_sessions SET revoked_at = now(), revoked_reason = @reason
WHERE id = @id AND revoked_at IS NULL;
