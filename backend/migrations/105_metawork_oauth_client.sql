-- Register the Metawork desktop app as a first-party public OAuth client.
-- Contract mirrors metacode-cli (see 099_metacode_oauth_contract.sql):
-- public client, no client_secret, PKCE S256 mandatory, loopback callback + device code.
-- `profile` is included so the app can call /api/v1/auth/oauth/userinfo to show the
-- signed-in identity; the app must still request it explicitly in `scope`, because an
-- omitted `scope` defaults to every scope registered on this row.

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
    'metawork-app',
    NULL,
    'public',
    'Metawork App',
    '[]'::jsonb,
    TRUE,
    '["metawork:use", "profile"]'::jsonb,
    'active'
)
ON CONFLICT (client_id) DO UPDATE
SET client_secret_hash = NULL,
    client_type = 'public',
    name = EXCLUDED.name,
    allow_loopback_redirect = TRUE,
    scopes = EXCLUDED.scopes,
    status = 'active',
    deleted_at = NULL,
    updated_at = NOW();
