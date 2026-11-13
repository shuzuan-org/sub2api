-- alert-triage 取证用的只读库账号：只能读 ops_error_logs 的分类列。
-- 在 111.229.235.75 上执行（执行前确认）：
--   sudo -u postgres psql -d sub2api_v2 -v pw="$(openssl rand -hex 24)" -f triage_ro.sql
-- 然后把密码写进 119 的 ~/server-prometheus/alert-triage/secrets/pg_dsn（见 119/alert-triage/README.md）。
-- 故意不授权 request_body / request_headers / client_ip / user_agent / error_body / user_id / api_key_id：
-- 这些证据会进私有仓库的 issue 并发给模型，带不出去就不会泄露。
CREATE ROLE triage_ro LOGIN PASSWORD :'pw' CONNECTION LIMIT 3;
ALTER ROLE triage_ro SET statement_timeout = '5s';
ALTER ROLE triage_ro SET default_transaction_read_only = on;
GRANT CONNECT ON DATABASE sub2api_v2 TO triage_ro;
GRANT USAGE ON SCHEMA public TO triage_ro;
GRANT SELECT (
  created_at, platform, model, account_id, error_phase, error_type, status_code,
  upstream_status_code, upstream_error_message, provider_error_code
) ON ops_error_logs TO triage_ro;
