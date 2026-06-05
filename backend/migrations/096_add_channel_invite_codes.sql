-- 渠道邀请码管理
-- 批次表、个体码表、批次-分组关联表、使用记录表

-- 渠道邀请码批次表
CREATE TABLE IF NOT EXISTS channel_invite_batches (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    bonus_amount DECIMAL(20,8) NOT NULL DEFAULT 0,
    max_uses_per_code INT NOT NULL DEFAULT 1,
    start_time TIMESTAMPTZ,
    end_time TIMESTAMPTZ,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    notes TEXT DEFAULT NULL,
    created_by BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 渠道邀请码表（个体码）
CREATE TABLE IF NOT EXISTS channel_invite_codes (
    id BIGSERIAL PRIMARY KEY,
    batch_id BIGINT NOT NULL REFERENCES channel_invite_batches(id) ON DELETE CASCADE,
    code VARCHAR(32) NOT NULL UNIQUE,
    status VARCHAR(20) NOT NULL DEFAULT 'unused',
    max_uses INT NOT NULL DEFAULT 1,
    used_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 批次-分组关联表
CREATE TABLE IF NOT EXISTS channel_invite_batch_groups (
    id BIGSERIAL PRIMARY KEY,
    batch_id BIGINT NOT NULL REFERENCES channel_invite_batches(id) ON DELETE CASCADE,
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    UNIQUE(batch_id, group_id)
);

-- 渠道邀请码使用记录表
CREATE TABLE IF NOT EXISTS channel_invite_code_usages (
    id BIGSERIAL PRIMARY KEY,
    code_id BIGINT NOT NULL REFERENCES channel_invite_codes(id) ON DELETE CASCADE,
    batch_id BIGINT NOT NULL REFERENCES channel_invite_batches(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    bonus_granted BOOLEAN NOT NULL DEFAULT FALSE,
    bonus_granted_at TIMESTAMPTZ,
    claimed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(code_id, user_id)
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_cib_status ON channel_invite_batches(status);
CREATE INDEX IF NOT EXISTS idx_cib_created_by ON channel_invite_batches(created_by);
CREATE INDEX IF NOT EXISTS idx_cic_batch_id ON channel_invite_codes(batch_id);
CREATE INDEX IF NOT EXISTS idx_cic_status ON channel_invite_codes(status);
CREATE INDEX IF NOT EXISTS idx_cic_code ON channel_invite_codes(code);
CREATE INDEX IF NOT EXISTS idx_cibg_batch_id ON channel_invite_batch_groups(batch_id);
CREATE INDEX IF NOT EXISTS idx_cibg_group_id ON channel_invite_batch_groups(group_id);
CREATE INDEX IF NOT EXISTS idx_cicu_code_id ON channel_invite_code_usages(code_id);
CREATE INDEX IF NOT EXISTS idx_cicu_user_id ON channel_invite_code_usages(user_id);
CREATE INDEX IF NOT EXISTS idx_cicu_bonus_not_granted ON channel_invite_code_usages(user_id, bonus_granted)
    WHERE bonus_granted = FALSE;

COMMENT ON TABLE channel_invite_batches IS '渠道邀请码批次';
COMMENT ON TABLE channel_invite_codes IS '渠道邀请码（个体码）';
COMMENT ON TABLE channel_invite_batch_groups IS '批次关联的目标分组';
COMMENT ON TABLE channel_invite_code_usages IS '渠道邀请码使用记录';
