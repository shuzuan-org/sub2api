-- 分组可见性：布尔 is_exclusive → 三档枚举 visibility（public / subscriber / private）
-- 并新增 group_visible_plans 关联表，用于 subscriber 档绑定的订阅计划集合。
--
-- 本迁移采用 expand-contract 模式的【expand 阶段】：只做加法，保留旧列 is_exclusive，
-- 使新旧二进制可共存——上线出问题可随时回退旧版本。旧列的删除由后续迁移 103 收尾，
-- 待新版本稳定运行、确认无需回滚后再执行。
--
-- 命名与 ent/migrate/schema.go 保持一致（索引名 group_visibility / groupvisibleplan_plan_id，
-- FK symbol group_visible_plans_groups_group / group_visible_plans_subscription_plans_plan）。

-- 1. 新增 visibility 列（默认 public）
ALTER TABLE groups ADD COLUMN IF NOT EXISTS visibility VARCHAR(20) NOT NULL DEFAULT 'public';

-- 2. 回填：is_exclusive=true → private；false → public（仅当旧列仍存在时）
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'groups' AND column_name = 'is_exclusive'
    ) THEN
        UPDATE groups SET visibility = CASE WHEN is_exclusive THEN 'private' ELSE 'public' END;
    END IF;
END $$;

-- 2b. DB 层兜底：visibility 只允许三档枚举值。
-- 应用层（ResolveVisibility + handler oneof binding）已把关，但 DB 约束是最后一道防线，
-- 防止脏 SQL / 未来未走 handler 的内部路径写入非法值导致 CanBindGroup 静默判为"不可见"。
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'groups_visibility_check'
    ) THEN
        ALTER TABLE groups ADD CONSTRAINT groups_visibility_check
            CHECK (visibility IN ('public', 'subscriber', 'private'));
    END IF;
END $$;

-- 3. 新增 visibility 索引。
-- 注意：旧列 is_exclusive 及其索引在本阶段【保留不动】，由迁移 103 删除。
CREATE INDEX IF NOT EXISTS group_visibility ON groups(visibility);

-- 4. subscriber 分组 ↔ 订阅计划 关联表（复合主键，与 user_allowed_groups 同范式）
CREATE TABLE IF NOT EXISTS group_visible_plans (
    group_id   BIGINT NOT NULL,
    plan_id    BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (group_id, plan_id),
    CONSTRAINT group_visible_plans_groups_group
        FOREIGN KEY (group_id) REFERENCES groups(id),
    CONSTRAINT group_visible_plans_subscription_plans_plan
        FOREIGN KEY (plan_id) REFERENCES subscription_plans(id)
);
CREATE INDEX IF NOT EXISTS groupvisibleplan_plan_id ON group_visible_plans(plan_id);
