-- Add optional PKCE fields for OAuth authorization codes.
ALTER TABLE oauth_authorization_codes
    ADD COLUMN IF NOT EXISTS code_challenge VARCHAR(128) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS code_challenge_method VARCHAR(10) NOT NULL DEFAULT '';

