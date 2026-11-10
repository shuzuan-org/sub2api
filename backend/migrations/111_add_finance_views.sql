-- 财务只读视图：给财务 MCP（大模型）查询用。
--
-- 设计：
-- 1. 财务账号 finance_ro 只授予 finance schema 的 SELECT；视图以属主身份读底层表，
--    所以凭据、密码、兑换码等列不放进视图，就从权限上不可见。
-- 2. 口径写在视图里（分→元、上海自然日、成本=原价×账号当前成本倍率），列注释写清含义，
--    大模型靠 COMMENT 理解表结构。
-- 3. usage_logs 很大，按天聚合放在物化视图 finance_internal.usage_daily_mv，
--    由生产机定时 REFRESH（见 deploy/finance/README.md）。迁移里建成 WITH NO DATA，
--    避免服务启动时扫全表；首次部署后必须先手工 REFRESH 一次。
-- 4. 账号 finance_ro 和授权不在本迁移里（sub2api 用户没有 CREATEROLE），
--    由 deploy/finance/finance_ro.sql 用 postgres 超级用户执行。
--
-- 注意：视图依赖的列，以后的迁移不能直接 DROP/改类型，需先 DROP 视图再重建。

CREATE SCHEMA IF NOT EXISTS finance;
CREATE SCHEMA IF NOT EXISTS finance_internal;

COMMENT ON SCHEMA finance IS '财务只读视图。金额单位：*_yuan 为人民币元，*_u 为系统额度 U（1U=0.1 元）。日期列均为上海自然日。';
COMMENT ON SCHEMA finance_internal IS '财务视图的内部支撑对象（物化视图），不授权给财务账号。';

-- ---------------------------------------------------------------------------
-- 充值订单
-- ---------------------------------------------------------------------------
CREATE VIEW finance.recharge_orders AS
SELECT
    o.id,
    o.order_no,
    o.alipay_trade_no,
    o.user_id,
    o.package_id,
    o.status,
    (o.cny_fee / 100.0)::numeric(12, 2)                   AS amount_yuan,
    o.usd_amount                                          AS credit_u,
    o.created_at,
    o.paid_at,
    (o.paid_at AT TIME ZONE 'Asia/Shanghai')::date        AS paid_date,
    o.expires_at
FROM public.alipay_orders o;

COMMENT ON VIEW finance.recharge_orders IS '支付宝充值订单，一行一笔。统计充值只看 status=''paid''，按 paid_date 分日。';
COMMENT ON COLUMN finance.recharge_orders.order_no IS '平台订单号';
COMMENT ON COLUMN finance.recharge_orders.alipay_trade_no IS '支付宝交易号，可与支付宝账单对账';
COMMENT ON COLUMN finance.recharge_orders.user_id IS '付款用户，对应 finance.users.id';
COMMENT ON COLUMN finance.recharge_orders.package_id IS '充值档位编号';
COMMENT ON COLUMN finance.recharge_orders.status IS 'paid=已付款；pending=未付款（含已过期未付）';
COMMENT ON COLUMN finance.recharge_orders.amount_yuan IS '订单金额，人民币元';
COMMENT ON COLUMN finance.recharge_orders.credit_u IS '付款后到账额度，单位 U';
COMMENT ON COLUMN finance.recharge_orders.created_at IS '下单时间';
COMMENT ON COLUMN finance.recharge_orders.paid_at IS '付款时间；未付款为空';
COMMENT ON COLUMN finance.recharge_orders.paid_date IS '付款日（上海自然日），按日统计充值用这一列';
COMMENT ON COLUMN finance.recharge_orders.expires_at IS '订单支付截止时间';

-- ---------------------------------------------------------------------------
-- 用户
-- ---------------------------------------------------------------------------
CREATE VIEW finance.users WITH (security_barrier = true) AS
SELECT
    u.id,
    regexp_replace(u.email, '^(.{1,2})[^@]*', '\1***')    AS email_masked,
    u.username,
    u.role,
    u.status,
    u.balance                                             AS balance_u,
    u.concurrency,
    CASE
        WHEN coalesce(nullif(u.phone_number, ''), nullif(u.phone, '')) IS NULL THEN NULL
        ELSE left(coalesce(nullif(u.phone_number, ''), u.phone), 3) || '****'
             || right(coalesce(nullif(u.phone_number, ''), u.phone), 4)
    END                                                   AS phone_masked,
    u.phone_verified,
    u.referred_by,
    u.created_at,
    (u.created_at AT TIME ZONE 'Asia/Shanghai')::date     AS created_date,
    u.deleted_at IS NOT NULL                              AS is_deleted
FROM public.users u;

COMMENT ON VIEW finance.users IS '用户及当前余额。邮箱、手机号已脱敏。统计在册用户时加 is_deleted = false。';
COMMENT ON COLUMN finance.users.email_masked IS '脱敏邮箱，如 su***@126.com';
COMMENT ON COLUMN finance.users.role IS 'admin=管理员；user=普通用户';
COMMENT ON COLUMN finance.users.status IS 'active=正常；其他为禁用等状态';
COMMENT ON COLUMN finance.users.balance_u IS '当前钱包余额，单位 U';
COMMENT ON COLUMN finance.users.concurrency IS '并发上限';
COMMENT ON COLUMN finance.users.phone_masked IS '脱敏手机号，如 138****1234';
COMMENT ON COLUMN finance.users.referred_by IS '邀请人的用户 id';
COMMENT ON COLUMN finance.users.created_date IS '注册日（上海自然日）';
COMMENT ON COLUMN finance.users.is_deleted IS '账号是否已删除';

-- ---------------------------------------------------------------------------
-- 余额 / 额度调整（兑换码与管理员调整共用 redeem_codes 表）
-- ---------------------------------------------------------------------------
CREATE VIEW finance.balance_adjustments AS
SELECT
    r.id,
    r.type,
    r.status,
    r.value,
    r.used_by                                             AS user_id,
    r.used_at,
    (r.used_at AT TIME ZONE 'Asia/Shanghai')::date        AS used_date,
    r.plan_id,
    r.validity_days,
    r.created_at
FROM public.redeem_codes r;

COMMENT ON VIEW finance.balance_adjustments IS '兑换码使用与管理员手动调整记录（不含兑换码本身）。只有 status=''used'' 的记录才真正生效到用户。';
COMMENT ON COLUMN finance.balance_adjustments.type IS 'balance=余额兑换码；admin_balance=管理员调余额（value 可为负）；subscription=套餐兑换码；concurrency/admin_concurrency=并发调整；invitation=邀请码';
COMMENT ON COLUMN finance.balance_adjustments.status IS 'used=已使用/已生效；unused=未使用';
COMMENT ON COLUMN finance.balance_adjustments.value IS 'balance/admin_balance 时为 U；concurrency 类为并发数；subscription 类见 plan_id';
COMMENT ON COLUMN finance.balance_adjustments.user_id IS '使用者 / 被调整的用户';
COMMENT ON COLUMN finance.balance_adjustments.used_date IS '生效日（上海自然日）';
COMMENT ON COLUMN finance.balance_adjustments.plan_id IS '套餐兑换码对应的套餐，见 finance.plans';
COMMENT ON COLUMN finance.balance_adjustments.validity_days IS '套餐兑换码的有效天数';

-- ---------------------------------------------------------------------------
-- 上游账号 / 分组
-- ---------------------------------------------------------------------------
CREATE VIEW finance.accounts AS
SELECT
    a.id,
    a.name,
    a.platform,
    a.type,
    a.status,
    a.schedulable,
    coalesce(a.rate_multiplier, 1)                        AS cost_rate,
    a.last_used_at,
    a.expires_at,
    a.created_at,
    a.deleted_at IS NOT NULL                              AS is_deleted
FROM public.accounts a;

COMMENT ON VIEW finance.accounts IS '上游账号（不含凭据和代理配置）。cost_rate 用于计算上游成本。';
COMMENT ON COLUMN finance.accounts.platform IS '上游平台，如 anthropic / openai / gemini';
COMMENT ON COLUMN finance.accounts.schedulable IS '当前是否参与调度';
COMMENT ON COLUMN finance.accounts.cost_rate IS '成本倍率（当前值）：上游成本 = 原价 × cost_rate。未录入折扣的账号为 1';
COMMENT ON COLUMN finance.accounts.is_deleted IS '账号是否已删除';

CREATE VIEW finance.groups AS
SELECT
    g.id,
    g.name,
    g.platform,
    g.status,
    g.rate_multiplier,
    g.created_at,
    g.deleted_at IS NOT NULL                              AS is_deleted
FROM public.groups g;

COMMENT ON VIEW finance.groups IS '用户分组。rate_multiplier 是向用户收费的倍率（实际计费 = 原价 × 倍率）。';
COMMENT ON COLUMN finance.groups.rate_multiplier IS '收费倍率';

-- ---------------------------------------------------------------------------
-- 用量：明细 + 按天聚合
-- ---------------------------------------------------------------------------
CREATE VIEW finance.usage_requests AS
SELECT
    l.id,
    l.created_at,
    l.user_id,
    l.api_key_id,
    l.account_id,
    l.group_id,
    l.subscription_id,
    lower(coalesce(nullif(trim(l.billing_model), ''), l.model)) AS model,
    l.requested_model,
    l.billing_type,
    l.input_tokens,
    l.output_tokens,
    l.cache_creation_tokens,
    l.cache_read_tokens,
    l.total_cost                                          AS list_price_u,
    l.actual_cost                                         AS revenue_u,
    l.rate_multiplier,
    l.duration_ms,
    l.stream,
    l.request_id
FROM public.usage_logs l;

COMMENT ON VIEW finance.usage_requests IS '逐请求用量明细，数据量很大：必须用 created_at 范围过滤（如 created_at >= ''2026-11-01'' AND created_at < ''2026-11-02''），按天汇总请改用 finance.usage_daily。';
COMMENT ON COLUMN finance.usage_requests.created_at IS '请求时间（有索引，过滤必须用这一列）';
COMMENT ON COLUMN finance.usage_requests.model IS '计费模型名（小写），与收入看板一致';
COMMENT ON COLUMN finance.usage_requests.requested_model IS '用户请求时填写的模型名';
COMMENT ON COLUMN finance.usage_requests.billing_type IS '0=余额计费（扣钱包）；1=套餐计费（扣订阅额度）';
COMMENT ON COLUMN finance.usage_requests.list_price_u IS '原价，单位 U';
COMMENT ON COLUMN finance.usage_requests.revenue_u IS '实际向用户计费，单位 U（原价 × 分组/用户倍率）';
COMMENT ON COLUMN finance.usage_requests.rate_multiplier IS '本次请求的收费倍率';
COMMENT ON COLUMN finance.usage_requests.duration_ms IS '请求耗时（毫秒）';

CREATE MATERIALIZED VIEW finance_internal.usage_daily_mv AS
SELECT
    (l.created_at AT TIME ZONE 'Asia/Shanghai')::date            AS day,
    l.user_id,
    coalesce(l.account_id, 0)                                    AS account_id,
    l.group_id,
    lower(coalesce(nullif(trim(l.billing_model), ''), l.model))  AS model,
    l.billing_type,
    count(*)                                                     AS requests,
    coalesce(sum(l.input_tokens), 0)                             AS input_tokens,
    coalesce(sum(l.output_tokens), 0)                            AS output_tokens,
    coalesce(sum(l.cache_creation_tokens), 0)                    AS cache_creation_tokens,
    coalesce(sum(l.cache_read_tokens), 0)                        AS cache_read_tokens,
    coalesce(sum(l.total_cost), 0)                               AS list_price_u,
    coalesce(sum(l.actual_cost), 0)                              AS revenue_u
FROM public.usage_logs l
GROUP BY 1, 2, 3, 4, 5, 6
WITH NO DATA;

-- 不阻塞读的并发刷新需要唯一索引；group_id 可为空，用 NULLS NOT DISTINCT（PG15+）。
-- （迁移校验器禁止普通迁移出现 C-O-N-C-U-R-R-E-N-T-L-Y 这个词，注释里也不行。）
CREATE UNIQUE INDEX usage_daily_mv_key
    ON finance_internal.usage_daily_mv (day, user_id, account_id, group_id, model, billing_type)
    NULLS NOT DISTINCT;
CREATE INDEX usage_daily_mv_day ON finance_internal.usage_daily_mv (day);

CREATE VIEW finance.usage_daily AS
SELECT
    d.day,
    d.user_id,
    d.account_id,
    d.group_id,
    d.model,
    d.billing_type,
    d.requests,
    d.input_tokens,
    d.output_tokens,
    d.cache_creation_tokens,
    d.cache_read_tokens,
    d.list_price_u,
    d.revenue_u,
    d.list_price_u * coalesce(a.rate_multiplier, 1)              AS cost_u
FROM finance_internal.usage_daily_mv d
LEFT JOIN public.accounts a ON a.id = d.account_id;

COMMENT ON VIEW finance.usage_daily IS '按「上海自然日 × 用户 × 上游账号 × 分组 × 模型 × 计费方式」聚合的用量，口径与后台收入看板一致。每小时刷新一次，当天数据可能滞后最多约 1 小时；需要实时数据时查 finance.usage_requests。';
COMMENT ON COLUMN finance.usage_daily.day IS '上海自然日';
COMMENT ON COLUMN finance.usage_daily.account_id IS '上游账号，见 finance.accounts；0 表示无账号';
COMMENT ON COLUMN finance.usage_daily.group_id IS '用户分组，见 finance.groups';
COMMENT ON COLUMN finance.usage_daily.model IS '计费模型名（小写）';
COMMENT ON COLUMN finance.usage_daily.billing_type IS '0=余额计费；1=套餐计费';
COMMENT ON COLUMN finance.usage_daily.requests IS '请求数';
COMMENT ON COLUMN finance.usage_daily.list_price_u IS '原价合计，单位 U';
COMMENT ON COLUMN finance.usage_daily.revenue_u IS '实际计费合计（收入），单位 U';
COMMENT ON COLUMN finance.usage_daily.cost_u IS '上游成本，单位 U = 原价 × 账号当前成本倍率（与收入看板一致）。毛利 = revenue_u - cost_u';

-- ---------------------------------------------------------------------------
-- 套餐与订阅
-- ---------------------------------------------------------------------------
CREATE VIEW finance.plans AS
SELECT
    p.id,
    p.name,
    p.description,
    p.status,
    p.visibility,
    p.price                                               AS price_u,
    p.daily_limit_usd                                     AS daily_limit_u,
    p.weekly_limit_usd                                    AS weekly_limit_u,
    p.monthly_limit_usd                                   AS monthly_limit_u,
    p.default_validity_days,
    p.created_at,
    p.deleted_at IS NOT NULL                              AS is_deleted
FROM public.subscription_plans p;

COMMENT ON VIEW finance.plans IS '套餐目录。';
COMMENT ON COLUMN finance.plans.price_u IS '售价，单位 U';
COMMENT ON COLUMN finance.plans.daily_limit_u IS '每日可用额度，单位 U';
COMMENT ON COLUMN finance.plans.weekly_limit_u IS '每周可用额度，单位 U';
COMMENT ON COLUMN finance.plans.monthly_limit_u IS '每月可用额度，单位 U';
COMMENT ON COLUMN finance.plans.default_validity_days IS '默认有效天数';

CREATE VIEW finance.subscriptions AS
SELECT
    s.id,
    s.user_id,
    s.plan_id,
    p.name                                                AS plan_name,
    s.status,
    s.starts_at,
    s.expires_at,
    s.assigned_at,
    s.daily_usage_usd                                     AS daily_usage_u,
    s.weekly_usage_usd                                    AS weekly_usage_u,
    s.monthly_usage_usd                                   AS monthly_usage_u,
    s.created_at,
    s.deleted_at IS NOT NULL                              AS is_deleted
FROM public.user_subscriptions s
LEFT JOIN public.subscription_plans p ON p.id = s.plan_id;

COMMENT ON VIEW finance.subscriptions IS '用户订阅记录，一行一份订阅。';
COMMENT ON COLUMN finance.subscriptions.status IS 'active=生效中；其他为过期/取消等';
COMMENT ON COLUMN finance.subscriptions.starts_at IS '生效时间';
COMMENT ON COLUMN finance.subscriptions.expires_at IS '到期时间';
COMMENT ON COLUMN finance.subscriptions.assigned_at IS '开通时间';
COMMENT ON COLUMN finance.subscriptions.daily_usage_u IS '当前日窗口已用额度，单位 U';
COMMENT ON COLUMN finance.subscriptions.weekly_usage_u IS '当前周窗口已用额度，单位 U';
COMMENT ON COLUMN finance.subscriptions.monthly_usage_u IS '当前月窗口已用额度，单位 U';

-- ---------------------------------------------------------------------------
-- 用户密钥（不含密钥本身）
-- ---------------------------------------------------------------------------
CREATE VIEW finance.api_keys AS
SELECT
    k.id,
    k.user_id,
    k.name,
    k.group_id,
    k.status,
    k.quota                                               AS quota_u,
    k.quota_used                                          AS quota_used_u,
    k.expires_at,
    k.last_used_at,
    k.created_at,
    k.deleted_at IS NOT NULL                              AS is_deleted
FROM public.api_keys k;

COMMENT ON VIEW finance.api_keys IS '用户的 API 密钥（不含密钥字符串）。';
COMMENT ON COLUMN finance.api_keys.quota_u IS '密钥额度上限，单位 U；0 或空表示不限';
COMMENT ON COLUMN finance.api_keys.quota_used_u IS '密钥已用额度，单位 U';

-- ---------------------------------------------------------------------------
-- 优惠码与渠道邀请（不含码本身）
-- ---------------------------------------------------------------------------
CREATE VIEW finance.promo_codes AS
SELECT
    c.id,
    c.bonus_amount                                        AS bonus_u,
    c.max_uses,
    c.used_count,
    c.status,
    c.expires_at,
    c.notes,
    c.created_at
FROM public.promo_codes c;

COMMENT ON VIEW finance.promo_codes IS '优惠码活动（不含码本身）。';
COMMENT ON COLUMN finance.promo_codes.bonus_u IS '每次使用赠送额度，单位 U';

CREATE VIEW finance.promo_code_usages AS
SELECT
    u.id,
    u.promo_code_id,
    u.user_id,
    u.bonus_amount                                        AS bonus_u,
    u.used_at,
    (u.used_at AT TIME ZONE 'Asia/Shanghai')::date        AS used_date
FROM public.promo_code_usages u;

COMMENT ON VIEW finance.promo_code_usages IS '优惠码使用记录，一行一次赠送。';
COMMENT ON COLUMN finance.promo_code_usages.bonus_u IS '实际赠送额度，单位 U';

CREATE VIEW finance.channel_invite_batches AS
SELECT
    b.id,
    b.name,
    b.bonus_amount                                        AS bonus_u,
    b.max_uses_per_code,
    b.status,
    b.start_time,
    b.end_time,
    b.require_phone_verification,
    b.notes,
    b.created_at
FROM public.channel_invite_batches b;

COMMENT ON VIEW finance.channel_invite_batches IS '渠道邀请批次（不含邀请码本身）。';
COMMENT ON COLUMN finance.channel_invite_batches.bonus_u IS '每个领取用户赠送额度，单位 U';

CREATE VIEW finance.channel_invite_usages AS
SELECT
    u.id,
    u.batch_id,
    u.code_id,
    u.user_id,
    u.bonus_granted,
    u.bonus_granted_at,
    u.claimed_at,
    (u.claimed_at AT TIME ZONE 'Asia/Shanghai')::date     AS claimed_date
FROM public.channel_invite_code_usages u;

COMMENT ON VIEW finance.channel_invite_usages IS '渠道邀请领取记录。bonus_granted=true 才表示赠送已到账。';
COMMENT ON COLUMN finance.channel_invite_usages.bonus_granted IS '赠送额度是否已发放';
