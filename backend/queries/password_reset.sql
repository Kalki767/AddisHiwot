-- name: CreateResetToken :exec
INSERT INTO password_reset_tokens (user_id, token_hash, created_at, expires_at)
VALUES (@user_id, @token_hash, @created_at, @expires_at);

-- name: FindResetToken :one
-- A token that is still usable: never used and not expired.
SELECT id, user_id FROM password_reset_tokens
WHERE token_hash = @token_hash AND used_at IS NULL AND expires_at > @now;

-- name: ConsumeResetToken :execrows
-- Only the first caller succeeds, so one token sets one password.
UPDATE password_reset_tokens SET used_at = @now WHERE id = @id AND used_at IS NULL;
