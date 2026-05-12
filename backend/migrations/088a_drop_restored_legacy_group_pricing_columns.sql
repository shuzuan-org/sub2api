-- 088a_drop_restored_legacy_group_pricing_columns.sql
-- 清理 087a 为兼容 088 而临时恢复的 legacy 列，保持最终 schema 与当前代码一致。

ALTER TABLE groups DROP COLUMN IF EXISTS daily_limit_usd;
ALTER TABLE groups DROP COLUMN IF EXISTS weekly_limit_usd;
ALTER TABLE groups DROP COLUMN IF EXISTS monthly_limit_usd;
ALTER TABLE groups DROP COLUMN IF EXISTS price;
