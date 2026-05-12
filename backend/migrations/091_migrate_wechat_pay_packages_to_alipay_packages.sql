-- 将历史充值套餐配置从 wechat_pay_packages 迁移到 alipay_packages。
-- 仅在 alipay_packages 尚未存在时复制，避免覆盖新配置。

INSERT INTO settings (key, value, updated_at)
SELECT 'alipay_packages', legacy.value, NOW()
FROM settings AS legacy
WHERE legacy.key = 'wechat_pay_packages'
  AND NOT EXISTS (
    SELECT 1 FROM settings AS current WHERE current.key = 'alipay_packages'
  )
  AND legacy.value IS NOT NULL
  AND legacy.value != '';
