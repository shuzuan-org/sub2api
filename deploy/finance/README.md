# 财务只读视图（finance schema）

给财务 MCP（大模型查数）用的只读数据层。数据库里新增两个 schema：

- `finance`：财务能查的视图，口径写在视图里，每列有中文 `COMMENT`。
- `finance_internal`：支撑对象（用量物化视图），不授权给财务账号。

视图由迁移 `backend/migrations/111_add_finance_views.sql` 创建，随服务启动自动执行。
账号和定时刷新需要在生产机上手工做一次，见下文。

## 视图清单

| 视图 | 内容 | 不包含 |
|---|---|---|
| `recharge_orders` | 支付宝充值订单（元、U、上海付款日） | 二维码、回调原文 |
| `users` | 用户、余额、注册日 | 密码、2FA、邀请码、微信；邮箱和手机号脱敏 |
| `balance_adjustments` | 兑换码使用 + 管理员调余额/并发 | 兑换码本身 |
| `accounts` | 上游账号、成本倍率 | 凭据、代理、extra |
| `groups` | 分组、收费倍率 | 路由配置 |
| `usage_requests` | 逐请求用量明细（必须按 `created_at` 过滤） | IP、UA、价格快照 |
| `usage_daily` | 按天 × 用户 × 账号 × 分组 × 模型 × 计费方式聚合，含原价/收入/成本 | — |
| `plans` / `subscriptions` | 套餐目录 / 用户订阅 | — |
| `api_keys` | 用户密钥名称、状态、额度 | 密钥字符串、IP 白名单 |
| `promo_codes` / `promo_code_usages` | 优惠码活动 / 使用记录 | 优惠码本身 |
| `channel_invite_batches` / `channel_invite_usages` | 渠道邀请批次 / 领取记录 | 邀请码本身 |

口径：

- 日期列都是上海自然日；`*_yuan` 为人民币元，`*_u` 为 U（1U = 0.1 元）。
- `usage_daily` 与后台收入看板一致：模型名 = `lower(coalesce(billing_model, model))`；
  成本 = 原价 × 账号**当前**成本倍率（`accounts.rate_multiplier`）。
- `usage_daily` 每小时刷新，当天数据最多滞后约 1 小时。

## 首次上线（111.229.235.75）

1. 发布包含迁移 111 的版本，确认迁移已执行：
   ```bash
   sudo -u postgres psql -d sub2api_v2 -Atc "select filename from schema_migrations where filename like '111_%'"
   ```
2. 首次填充用量物化视图并安装定时刷新：
   ```bash
   sudo install -d /opt/sub2api/finance
   sudo install -m 755 refresh-usage-daily.sh /opt/sub2api/finance/
   sudo install -m 644 finance-usage-refresh.service finance-usage-refresh.timer /etc/systemd/system/
   sudo systemctl daemon-reload
   sudo systemctl start finance-usage-refresh.service     # 首次填充，扫一遍 usage_logs
   sudo systemctl enable --now finance-usage-refresh.timer
   ```
3. 建只读账号（密码只写进财务 MCP 服务的环境文件）：
   ```bash
   sudo -u postgres psql -d sub2api_v2 -v pw="$(openssl rand -hex 24)" -f finance_ro.sql
   ```

在步骤 2 完成之前查询 `finance.usage_daily` 会报 `materialized view ... has not been populated`。

## 财务 MCP 服务：DBHub

用第三方 MCP server [DBHub](https://github.com/bytebase/dbhub)（npm `@bytebase/dbhub`，锁定 1.5.0）以 `finance_ro` 连库，
财务在 Codex 里填网址 + 令牌即可用大模型查数。配置模板：`dbhub.toml.example`。

提供给大模型的工具：

| 工具 | 作用 |
|---|---|
| `search_objects` | 列出 finance 下的视图、列及中文注释（`object_type` 用 `view` / `column`） |
| `execute_sql` | 任意只读 SQL，最多返回 1000 行，15 秒超时 |
| `recharge_summary` | 固定口径：按上海付款日汇总已付支付宝充值 |
| `usage_summary` | 固定口径：按天汇总请求数、原价、收入、成本、毛利 |

启动（只监听本机，由反向代理对外暴露 `/mcp`）：

```bash
DBHUB_AUTH_TOKEN="<令牌1>,<令牌2>" \
  npx @bytebase/dbhub@1.5.0 --config /etc/dbhub/dbhub.toml --transport http --port 8090
```

安全层次（本机已逐项实测）：

1. DBHub Bearer 令牌：无令牌访问 `/mcp`、Workbench 页面、`/api/requests` 均为 401。多个令牌用逗号分隔，每人一个。
2. DBHub 只读模式：`execute_sql` 只放行 select / with / explain / show，拒绝 UPDATE、多语句、`COMMIT; BEGIN READ WRITE`、带 DELETE 的 CTE。
3. 连接级 `default_transaction_read_only=on` + 每条查询包在 `BEGIN READ ONLY` 里。
4. 数据库权限：`finance_ro` 读 `public` 原始表、`finance_internal` 均 permission denied；即使开读写事务也写不进。

注意：

- 自定义工具（`recharge_summary` 等）不受 DBHub 只读模式保护，语句必须自己写成只读（最终仍由第 4 层兜底）。
- 反向代理只转发 `/mcp`。Workbench 和 `/api/requests` 这两个路径虽然要令牌，也不要暴露出去。
- DBHub 的 Host 头检查默认只放行 localhost，经反向代理用域名访问时要加 `--allowed-hosts <域名>`。

## 维护注意

- 视图依赖的列，以后的迁移不能直接 `DROP COLUMN` / 改类型：先 `DROP VIEW`，改完再重建（新迁移里写）。
- 在 `finance` 下新增视图会自动授权给 `finance_ro`（默认权限），不用重跑 `finance_ro.sql`。
- 生产库有几列代码里没有定义的历史遗留列（`groups.is_subscription` / `subscription_type` /
  `default_validity_days`、`subscription_plans.source_group_id`、`user_subscriptions.group_id`），
  视图刻意不使用它们。
