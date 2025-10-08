-- name: GetIdentityUserID :one
SELECT user_id FROM user_identities WHERE provider = @provider AND subject = @subject;

-- name: CreateIdentity :execrows
-- Does nothing if the provider account or the user is already linked; the
-- caller then checks who holds the link.
INSERT INTO user_identities (user_id, provider, subject, created_at)
VALUES (@user_id, @provider, @subject, @created_at)
ON CONFLICT DO NOTHING;
