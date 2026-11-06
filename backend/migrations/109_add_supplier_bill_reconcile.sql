-- 收入看板「月度对账」：供应商账号绑定 + 财务每月导入的供应商账单。
-- 纯管理端报表数据，不参与网关调度/计费，所以不建 ent schema，仓储走裸 SQL。

-- 一个 sub2api 账号最多绑定一个供应商账号；同一供应商账号可被多个 sub2api 账号共用。
CREATE TABLE IF NOT EXISTS account_supplier_bindings (
    account_id       BIGINT       PRIMARY KEY,
    supplier         VARCHAR(100) NOT NULL,
    supplier_account VARCHAR(200) NOT NULL,
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- 一行 = 账期 × 供应商账号 × 标准模型；standard_model = '调账' 表示整笔调账行。
-- 数值列允许 NULL：模板约定「未取得的值留空，不要填 0」，空与 0 要能区分。
CREATE TABLE IF NOT EXISTS supplier_bill_lines (
    id                 BIGSERIAL     PRIMARY KEY,
    period             CHAR(7)       NOT NULL, -- YYYY-MM，北京时间自然月
    supplier           VARCHAR(100)  NOT NULL DEFAULT '',
    supplier_account   VARCHAR(200)  NOT NULL DEFAULT '',
    standard_model     VARCHAR(200)  NOT NULL,
    platform_models    TEXT          NOT NULL DEFAULT '', -- 平台模型原名/别名，逗号分隔
    cost_discount      NUMERIC(10,4),                     -- 当期历史成本折扣；空 = 用账号当前折扣
    currency           VARCHAR(16)   NOT NULL DEFAULT '',
    input_tokens       NUMERIC(20,3),
    output_tokens      NUMERIC(20,3),
    cache_read_tokens  NUMERIC(20,3),
    cache_write_tokens NUMERIC(20,3),
    other_tokens       NUMERIC(20,3),
    resource_fee       NUMERIC(20,6),
    discount_amount    NUMERIC(20,6),
    adjustment         NUMERIC(20,6),
    tax                NUMERIC(20,6),
    fx_rate            NUMERIC(20,8),
    note               TEXT          NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_supplier_bill_lines_period ON supplier_bill_lines (period);
