-- The old check does not know 'google_linked'; those sessions ended because the
-- password was dropped, the closest older reason.
UPDATE auth_sessions SET revoked_reason = 'password_reset' WHERE revoked_reason = 'google_linked';
ALTER TABLE auth_sessions DROP CONSTRAINT auth_sessions_revoked_reason_known;
ALTER TABLE auth_sessions ADD CONSTRAINT auth_sessions_revoked_reason_known
    CHECK (revoked_reason IN ('logout', 'reuse_detected', 'password_reset', 'account_deleted'));

DROP TABLE user_identities;
