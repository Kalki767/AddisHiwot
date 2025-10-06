-- Remember which token replaced each refresh token. When a used token comes back
-- within a short grace period and its replacement was never used, the client
-- most likely lost the refresh response on a bad network and retried; that is
-- answered with a fresh token instead of ending the session.
ALTER TABLE refresh_tokens
    ADD COLUMN replaced_by uuid REFERENCES refresh_tokens (id) ON DELETE SET NULL;
