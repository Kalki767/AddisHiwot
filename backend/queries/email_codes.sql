-- name: LockUserCodes :exec
-- Serialises issuing per person and purpose for the rest of the transaction, so
-- two requests cannot both retire the live code and then insert a new one (the
-- second insert would break the "one live code" index).
SELECT pg_advisory_xact_lock(hashtextextended(@key::text, 0));

-- name: RetireLiveCodes :exec
-- A new code replaces the live one, so only the latest email works.
UPDATE email_codes SET consumed_at = @now
WHERE user_id = @user_id AND purpose = @purpose AND consumed_at IS NULL;

-- name: CreateEmailCode :exec
INSERT INTO email_codes (user_id, purpose, code_hash, created_at, expires_at)
VALUES (@user_id, @purpose, @code_hash, @now, @expires_at);

-- name: CountEmailCodesSince :one
SELECT count(*) FROM email_codes
WHERE user_id = @user_id AND purpose = @purpose AND created_at >= @since;

-- name: UseEmailCodeAttempt :one
-- Counts one attempt on the live, unexpired code that still has attempts left,
-- and returns its hash to compare. The attempt is counted before the comparison
-- and in one statement, so parallel guesses cannot get more than max_attempts.
UPDATE email_codes SET attempts = attempts + 1
WHERE user_id = @user_id AND purpose = @purpose AND consumed_at IS NULL
  AND expires_at > @now AND attempts < @max_attempts::integer
RETURNING id, code_hash;

-- name: ConsumeEmailCode :execrows
UPDATE email_codes SET consumed_at = @now WHERE id = @id AND consumed_at IS NULL;
