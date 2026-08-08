-- Register the Metacodes web application as its own confidential OAuth client.
--
-- The application is currently served over a bare IP address. Keep the exact
-- callback allowlisted and continue to require PKCE S256. The client secret is
-- consumed only by the Metacodes backend during token exchange; replace the
-- HTTP callback with HTTPS before treating this integration as secure on an
-- untrusted network.
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
    'metacodes-web',
    '$2y$12$8v5Ein9pAxxHi7xdYz87/upSsHkEehEp3kT5MqStvJsuaCaOp2BAi',
    'confidential',
    'Metacodes Web',
    '["http://58.211.6.130:10510/auth/callback"]'::jsonb,
    FALSE,
    '["metacode:use", "profile"]'::jsonb,
    'active'
)
ON CONFLICT (client_id) DO UPDATE
SET client_secret_hash = EXCLUDED.client_secret_hash,
    client_type = 'confidential',
    name = EXCLUDED.name,
    redirect_uris = EXCLUDED.redirect_uris,
    allow_loopback_redirect = FALSE,
    scopes = EXCLUDED.scopes,
    status = 'active',
    updated_at = NOW();
