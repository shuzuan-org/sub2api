ALTER TABLE channel_invite_batches
    ADD COLUMN IF NOT EXISTS require_phone_verification BOOLEAN NOT NULL DEFAULT TRUE;
