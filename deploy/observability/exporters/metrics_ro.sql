-- postgres_exporter / sql_exporter 用的只读库账号（2026-11-04 决定新建，不扩 grafana_ro）。
-- 在 111.229.235.75 上执行（执行前确认）：
--   sudo -u postgres psql -d sub2api_v2 -v pw="$(openssl rand -hex 24)" -f metrics_ro.sql
-- 然后把同一个密码写进 /etc/postgres_exporter/dsn 与 /etc/sql_exporter/dsn（见 README）。
-- 只给 sub2api.collector.yml 用到的表 / 列；pg_monitor 给 postgres_exporter 读 pg_stat_*。
CREATE ROLE metrics_ro LOGIN PASSWORD :'pw' CONNECTION LIMIT 5;
ALTER ROLE metrics_ro SET statement_timeout = '8s';
ALTER ROLE metrics_ro SET default_transaction_read_only = on;
GRANT pg_monitor TO metrics_ro;
GRANT CONNECT ON DATABASE sub2api_v2 TO metrics_ro;
GRANT USAGE ON SCHEMA public TO metrics_ro;
GRANT SELECT (created_at) ON usage_logs TO metrics_ro;
GRANT SELECT (status, expires_at, deleted_at) ON user_subscriptions TO metrics_ro;
GRANT SELECT (status, created_at, paid_at, cny_fee) ON alipay_orders TO metrics_ro;
GRANT SELECT (platform, type, status, schedulable, deleted_at) ON accounts TO metrics_ro;
GRANT SELECT (id, name, platform, status, deleted_at) ON groups TO metrics_ro;
