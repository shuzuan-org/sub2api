-- 094_add_user_phone.sql
-- Add optional phone_number binding for SMS verification-code login (E.164 format, e.g. +8613800138000).

ALTER TABLE users ADD COLUMN IF NOT EXISTS phone_number VARCHAR(32);
ALTER TABLE users ADD COLUMN IF NOT EXISTS phone_bound_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS phone_bonus_granted_at TIMESTAMPTZ;

-- Only active (not soft-deleted) users with a non-empty phone_number must be unique.
-- ent schema also declares phone_number as Unique, so this partial index supplements soft delete.
CREATE UNIQUE INDEX IF NOT EXISTS users_phone_number_unique_active
    ON users(phone_number)
    WHERE deleted_at IS NULL AND phone_number IS NOT NULL AND phone_number <> '';
