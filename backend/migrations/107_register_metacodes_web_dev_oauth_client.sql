-- Register a separate public OAuth client for local Metacodes Web development.
--
-- Loopback redirects may use any explicit local port, but the callback path must
-- remain /auth/callback. PKCE S256 is still required by the OAuth server.
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
    'metacodes-web-dev',
    NULL,
    'public',
    'Metacodes Web Dev',
    '[]'::jsonb,
    TRUE,
    '["metacode:use", "profile"]'::jsonb,
    'active'
)
ON CONFLICT (client_id) DO UPDATE
SET client_secret_hash = NULL,
    client_type = 'public',
    name = EXCLUDED.name,
    redirect_uris = EXCLUDED.redirect_uris,
    allow_loopback_redirect = TRUE,
    scopes = EXCLUDED.scopes,
    status = 'active',
    updated_at = NOW();
