-- 094: Add phone and phone_verified fields for phone-based login support.
-- Phone uniqueness is enforced via a partial index, compatible with soft delete.

ALTER TABLE users ADD COLUMN IF NOT EXISTS phone VARCHAR(32) DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS phone_verified BOOLEAN NOT NULL DEFAULT FALSE;

-- Partial unique index: only active (non-deleted) records with a non-empty phone must be unique.
-- Uses COALESCE instead of IS DISTINCT FROM to work as a WHERE condition.
CREATE UNIQUE INDEX IF NOT EXISTS users_phone_unique_active
ON users (phone)
WHERE deleted_at IS NULL AND phone IS NOT NULL AND phone <> '';
