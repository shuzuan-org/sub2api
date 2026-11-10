-- 财务 MCP 用的只读库账号。依赖迁移 111_add_finance_views.sql 已执行（finance schema 存在）。
-- 在 111.229.235.75 上执行（执行前确认）：
--   sudo -u postgres psql -d sub2api_v2 -v pw="$(openssl rand -hex 24)" -f finance_ro.sql
-- 密码只写进财务 MCP 服务的环境文件，不进仓库。
--
-- 权限模型：只给 finance schema 的 USAGE + SELECT，不给 public schema 任何表。
-- 视图以属主（sub2api）身份读底层表，所以 finance_ro 看不到视图之外的任何列。
-- finance_internal（物化视图）不授权，只能经 finance.usage_daily 访问。
CREATE ROLE finance_ro LOGIN PASSWORD :'pw' CONNECTION LIMIT 5;
ALTER ROLE finance_ro SET default_transaction_read_only = on;
ALTER ROLE finance_ro SET statement_timeout = '15s';
ALTER ROLE finance_ro SET idle_in_transaction_session_timeout = '30s';
ALTER ROLE finance_ro SET timezone = 'Asia/Shanghai';

SELECT format('GRANT CONNECT ON DATABASE %I TO finance_ro', current_database()) \gexec
GRANT USAGE ON SCHEMA finance TO finance_ro;
GRANT SELECT ON ALL TABLES IN SCHEMA finance TO finance_ro;

-- 以后迁移在 finance 下新建的视图自动授权，不用再跑本脚本。
ALTER DEFAULT PRIVILEGES FOR ROLE sub2api IN SCHEMA finance GRANT SELECT ON TABLES TO finance_ro;
