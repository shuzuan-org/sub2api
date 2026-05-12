-- 087a_restore_legacy_group_pricing_columns.sql
-- 为 088_convert_usd_to_u_tokens.sql 提供兼容列。
-- 082_subscription_plan_refactor.sql 已删除 groups 上的旧订阅价格/限额字段，
-- 但 088 仍会对这些列做一次预防性 UPDATE。这里先临时补回，供 088 顺利执行，
-- 后续由 088a 再删除，避免修改已发布 migration 的 checksum。

ALTER TABLE groups ADD COLUMN IF NOT EXISTS daily_limit_usd DECIMAL(20,8);
ALTER TABLE groups ADD COLUMN IF NOT EXISTS weekly_limit_usd DECIMAL(20,8);
ALTER TABLE groups ADD COLUMN IF NOT EXISTS monthly_limit_usd DECIMAL(20,8);
ALTER TABLE groups ADD COLUMN IF NOT EXISTS price DECIMAL(20,8);
