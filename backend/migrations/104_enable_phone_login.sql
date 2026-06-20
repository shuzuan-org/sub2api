-- Enable phone login/registration for existing deployments when the switch is absent.
-- Admins can still disable it later from system settings.
INSERT INTO settings (key, value, updated_at)
VALUES ('phone_login_enabled', 'true', NOW())
ON CONFLICT (key) DO NOTHING;
