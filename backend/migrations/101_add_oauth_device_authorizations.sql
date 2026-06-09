-- Add OAuth 2.0 Device Authorization Grant sessions for MetaCode CLI login.
CREATE TABLE IF NOT EXISTS oauth_device_authorizations (
    id BIGSERIAL PRIMARY KEY,
    device_code_hash CHAR(64) NOT NULL UNIQUE,
    user_code_hash CHAR(64) NOT NULL UNIQUE,
    client_id VARCHAR(128) NOT NULL,
    user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    scopes JSONB NOT NULL DEFAULT '[]'::jsonb,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    expires_at TIMESTAMPTZ NOT NULL,
    interval_seconds INT NOT NULL DEFAULT 5,
    last_poll_at TIMESTAMPTZ,
    poll_count INT NOT NULL DEFAULT 0,
    device_name VARCHAR(255) NOT NULL DEFAULT '',
    cli_version VARCHAR(64) NOT NULL DEFAULT '',
    platform VARCHAR(64) NOT NULL DEFAULT '',
    approved_at TIMESTAMPTZ,
    denied_at TIMESTAMPTZ,
    cancelled_at TIMESTAMPTZ,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_oauth_device_authorizations_client_status ON oauth_device_authorizations(client_id, status);
CREATE INDEX IF NOT EXISTS idx_oauth_device_authorizations_expires_at ON oauth_device_authorizations(expires_at);
CREATE INDEX IF NOT EXISTS idx_oauth_device_authorizations_user_id ON oauth_device_authorizations(user_id);

