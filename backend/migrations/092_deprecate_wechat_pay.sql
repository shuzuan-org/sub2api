-- 下线微信支付：保留历史订单数据，但脱钩 schema 与配置。
-- 改名而非 DROP，保留审计/历史订单可查询能力；后续如确认无需可再手动 DROP。

ALTER TABLE IF EXISTS wechat_pay_orders RENAME TO wechat_pay_orders_deprecated;

DELETE FROM settings
WHERE key IN ('wechat_pay_config', 'wechat_pay_enabled', 'wechat_pay_packages');
