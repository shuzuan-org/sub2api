-- OAuth grants are user-level. API key selection is handled as a post-auth profile choice.

DROP INDEX IF EXISTS idx_oauth_authorization_codes_api_key_id;
ALTER TABLE oauth_authorization_codes DROP COLUMN IF EXISTS api_key_id;

DROP INDEX IF EXISTS idx_oauth_refresh_tokens_api_key_id;
ALTER TABLE oauth_refresh_tokens DROP COLUMN IF EXISTS api_key_id;
