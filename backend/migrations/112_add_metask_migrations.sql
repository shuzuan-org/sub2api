-- 迁移到 metask-ai：每个用户一条记录。
-- status = frozen：本站已清零余额、停用 Key、终止订阅，等待 metask-ai 入账（失败会自动重试）；
-- status = completed：metask-ai 已入账，账号已停用。
-- payload 是冻结那一刻的快照（发给 metask-ai 的请求体，Key 只存 sha256），result 是 metask-ai 的返回。
CREATE TABLE IF NOT EXISTS metask_migrations (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL UNIQUE REFERENCES users(id),
    status VARCHAR(20) NOT NULL DEFAULT 'frozen',
    payload JSONB NOT NULL,
    result JSONB,
    attempts INT NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_metask_migrations_status ON metask_migrations(status, updated_at);
