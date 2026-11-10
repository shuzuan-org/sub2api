#!/usr/bin/env bash
# 刷新财务用量物化视图。以 postgres 用户运行（systemd 单元 finance-usage-refresh.service）。
# 首次（未填充）只能普通 REFRESH；之后用 CONCURRENTLY，刷新期间财务查询不被阻塞。
set -euo pipefail
DB="${DB:-sub2api_v2}"
MV="finance_internal.usage_daily_mv"

populated="$(psql -d "$DB" -Atc "select relispopulated from pg_class where oid = '${MV}'::regclass")"
if [ "$populated" = "t" ]; then
  psql -d "$DB" -v ON_ERROR_STOP=1 -c "REFRESH MATERIALIZED VIEW CONCURRENTLY ${MV}"
else
  psql -d "$DB" -v ON_ERROR_STOP=1 -c "REFRESH MATERIALIZED VIEW ${MV}"
fi
