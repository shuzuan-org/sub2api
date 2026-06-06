-- Align the OAuth server with the Metacode CLI public-client contract.

ALTER TABLE oauth_clients
    ALTER COLUMN client_secret_hash DROP NOT NULL,
    ADD COLUMN IF NOT EXISTS client_type VARCHAR(20) NOT NULL DEFAULT 'confidential',
    ADD COLUMN IF NOT EXISTS allow_loopback_redirect BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE oauth_clients
SET client_type = 'confidential'
WHERE client_type = '';

ALTER TABLE oauth_authorization_codes
    ADD COLUMN IF NOT EXISTS hmac_key_id VARCHAR(64) NOT NULL DEFAULT 'default',
    ADD COLUMN IF NOT EXISTS api_key_id BIGINT REFERENCES api_keys(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_oauth_authorization_codes_hmac_key_id ON oauth_authorization_codes(hmac_key_id);
CREATE INDEX IF NOT EXISTS idx_oauth_authorization_codes_api_key_id ON oauth_authorization_codes(api_key_id);

CREATE TABLE IF NOT EXISTS oauth_refresh_tokens (
    id BIGSERIAL PRIMARY KEY,
    token_hash CHAR(64) NOT NULL UNIQUE,
    hmac_key_id VARCHAR(64) NOT NULL DEFAULT 'default',
    family_id VARCHAR(64) NOT NULL,
    parent_token_hash CHAR(64),
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    api_key_id BIGINT REFERENCES api_keys(id) ON DELETE SET NULL,
    client_id VARCHAR(128) NOT NULL,
    scopes JSONB NOT NULL DEFAULT '[]'::jsonb,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_oauth_refresh_tokens_token_hash ON oauth_refresh_tokens(token_hash);
CREATE INDEX IF NOT EXISTS idx_oauth_refresh_tokens_family_id ON oauth_refresh_tokens(family_id);
CREATE INDEX IF NOT EXISTS idx_oauth_refresh_tokens_user_id ON oauth_refresh_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_oauth_refresh_tokens_api_key_id ON oauth_refresh_tokens(api_key_id);
CREATE INDEX IF NOT EXISTS idx_oauth_refresh_tokens_client_id ON oauth_refresh_tokens(client_id);
CREATE INDEX IF NOT EXISTS idx_oauth_refresh_tokens_status ON oauth_refresh_tokens(status);
CREATE INDEX IF NOT EXISTS idx_oauth_refresh_tokens_expires_at ON oauth_refresh_tokens(expires_at);

INSERT INTO oauth_clients (
    client_id,
    client_secret_hash,
    client_type,
    name,
    redirect_uris,
    allow_loopback_redirect,
    scopes,
    status
)
VALUES (
    'metacode-cli',
    NULL,
    'public',
    'Metacode CLI',
    '[]'::jsonb,
    TRUE,
    '["metacode:use"]'::jsonb,
    'active'
)
ON CONFLICT (client_id) DO UPDATE
SET client_secret_hash = NULL,
    client_type = 'public',
    name = EXCLUDED.name,
    allow_loopback_redirect = TRUE,
    scopes = EXCLUDED.scopes,
    status = 'active',
    updated_at = NOW();
