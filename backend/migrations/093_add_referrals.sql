-- 邀请好友（Referral）：在 users 表上加专属邀请码与邀请人关系。
-- 幂等：可重复执行。referral_code 懒创建（首次访问邀请页时生成），
-- 唯一约束用 partial index（仅对非 NULL 生效），与现有软删除唯一索引风格一致。

ALTER TABLE users ADD COLUMN IF NOT EXISTS referral_code VARCHAR(6);
ALTER TABLE users ADD COLUMN IF NOT EXISTS referred_by BIGINT;

CREATE UNIQUE INDEX IF NOT EXISTS uq_users_referral_code
    ON users (referral_code)
    WHERE referral_code IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_users_referred_by
    ON users (referred_by);
