# sub2api 可观测与告警

部署身份 **sub2api**：生产机 `111.229.235.75`，公网 `napi.metask-ai.com`。监控体系照 metask-ai（`infra/monitoring/`）迁移，
接到监控机 **119.45.6.42** 的共用 Prometheus / Alertmanager / blackbox 上（2026-11-04 起）。

| 场景 | 栈 | 告警 |
| --- | --- | --- |
| 生产告警 | 119 的共用栈：Prometheus 经 SSH 隧道抓 111.229 的 `/metrics` + 两个 exporter，blackbox 探公网 | 只给 `severity=critical` 发邮件（`[sub2api CRITICAL] …`），warning 只在 Alertmanager / Prometheus Alerts 页可见 |
| 本机验收 / 排障看板 | `../docker-compose.observability.yml`（Prometheus 9090 + Grafana 3001 + PG 下钻），经公网 HTTPS + Bearer 抓 `/metrics` | **不发信**：不接 Alertmanager，规则同一份，firing 只在本机 `/alerts` 页可见 |

## 文件

| 文件 | 用在哪 |
| --- | --- |
| `prometheus.rules.yml` | 规则（两处共用）；119 上挂成 `/etc/prometheus/sub2api-alerts.yml` |
| `119/prometheus-scrape.snippet.yml` | 并入 119 `~/server-prometheus/prometheus/prometheus.yml` 的抓取 job 与 `rule_files` |
| `119/tunnel-sub2api-11122923575.service` | 119 上的 systemd 隧道：docker0 `9111/9112/9113` → 111.229 `8080/9187/9399` |
| `exporters/postgres_exporter.service`、`exporters/sql_exporter.service` | 111.229 上的 systemd 单元（二进制，同 node_exporter 的装法，只绑 127.0.0.1） |
| `exporters/sql_exporter.yml`、`exporters/sub2api.collector.yml` | sql_exporter 配置与业务不变量查询（`sub2api_biz_*`） |
| `exporters/metrics_ro.sql` | 两个 exporter 共用的只读库账号（列级 SELECT + `pg_monitor`） |
| `119/alert-triage/` | 告警自动分诊 + Claude Code 自动修复（接 Alertmanager webhook），说明见该目录 README |
| `exporters/triage_ro.sql` | alert-triage 取证用的只读库账号（只能读 `ops_error_logs` 的分类列，可选） |
| `prometheus.yml`、`grafana/` | 本机验收栈 |

## 指标来源

| 来源 | 端点（111.229） | 119 抓取 | 前缀 / 内容 |
| --- | --- | --- | --- |
| sub2api 进程 | `127.0.0.1:8080/metrics`（不经 nginx，不需要 bearer） | job `sub2api`，`172.17.0.1:9111` | `sub2api_*`：HTTP、ops 错误分类、上游状态码（含 402 独立桶）、TTFT、账号池、连接池、token、**合成探针 `sub2api_probe_*`** |
| postgres_exporter | `127.0.0.1:9187` | job `sub2api-postgres`，`9112` | `pg_*`：连通、连接数、锁、事务、库大小 |
| sql_exporter | `127.0.0.1:9399` | job `sub2api-sql`，`9113`，60s | `sub2api_biz_*`：近 15 分钟 usage_logs 条数、订阅按状态 / 过期仍 active、支付宝订单（只导不告警）、账号按平台 / 状态 / 可调度、分组目录快照 |
| blackbox（119） | — | `blackbox-http`（`/health`）、`blackbox-napi-ready`（`/ready` body `"status":"ready"`）、`blackbox-napi-gate`（`/v1/models` 401 JSON `API_KEY_REQUIRED`） | `probe_*`，证书到期 `probe_ssl_earliest_cert_expiry` |
| node_exporter | `*:9100` | 早已在抓：job `node`，`9103` | 主机磁盘 / 内存（规则在 metask-ai 的 `HostDisk* / HostMemoryLow`，覆盖 node 组全部机器） |

## 生产接线（一次性；每一步都会改生产或共用设施，执行前确认）

### 111.229.235.75

1. 只读库账号：`sudo -u postgres psql -d sub2api_v2 -v pw="'<随机密码>'" -f exporters/metrics_ro.sql`。
2. exporter：下载 `postgres_exporter` v0.20.1、`sql_exporter` 0.24.11 官方 release 到 `/usr/local/bin/`；
   `sudo useradd --system --no-create-home --shell /usr/sbin/nologin postgres_exporter`（sql_exporter 同理）；
   `/etc/postgres_exporter/env`、`/etc/sql_exporter/env` 写 DSN（0600，属主各自用户，格式见 unit 文件头注）；
   `exporters/sql_exporter.yml` 与 `sub2api.collector.yml` 复制到 `/etc/sql_exporter/`；
   unit 复制到 `/etc/systemd/system/` 后 `daemon-reload && enable --now`。
   验证：`curl -s 127.0.0.1:9187/metrics | grep '^pg_up'`、`curl -s 127.0.0.1:9399/metrics | grep '^sub2api_biz_'`。
3. 隧道授权：`~/.ssh/authorized_keys` 里 119 那把 `tunnel_key` 当前是
   `restrict,port-forwarding,permitopen="127.0.0.1:9100",command="/bin/false"`，追加
   `,permitopen="127.0.0.1:8080",permitopen="127.0.0.1:9187",permitopen="127.0.0.1:9399"`（改前备份该文件）。

### 119.45.6.42（`~/server-prometheus`）

4. `sudo cp 119/tunnel-sub2api-11122923575.service /etc/systemd/system/ && sudo systemctl daemon-reload && sudo systemctl enable --now tunnel-sub2api-11122923575`。
5. **先备份** `prometheus/prometheus.yml` 与 `docker-compose.yml`，按 `119/prometheus-scrape.snippet.yml` 并入；
   `prometheus.rules.yml` 用 `scp` 复制为 `prometheus/sub2api-alerts.yml`（scp 保留 inode；**不要用 `sed -i`**，换 inode 后容器读的仍是旧文件），
   compose 加 mount 后 `docker compose up -d prometheus`（新增 mount 要重建容器；之后只改规则时 `docker compose kill -s HUP prometheus` 即可）。
6. 前置的跨仓库改动必须已同步到 119（见下「与现有监控不冲突」），否则 tassub2api / Metask 的规则会命中 sub2api 的指标。
7. 核对：`http://119.45.6.42:9090/targets` 里 `sub2api`、`sub2api-postgres`、`sub2api-sql`、`blackbox-napi-*` 都 UP；
   `curl -s 127.0.0.1:9090/api/v1/rules | jq '[.data.groups[] | select(.name|startswith("sub2api-")) | .rules | length] | add'` 应为 28（含 tassub2api 的 sub2api-* 组时数字更大，按 file 字段过滤）。
8. 邮件通路验证：`docker compose exec alertmanager amtool alert add --alertmanager.url=http://127.0.0.1:9093 alertname=TestCritical severity=critical env=prod service=sub2api --annotation=summary=邮件通道测试`，30 秒内收到 `[sub2api CRITICAL] 1 条告警：TestCritical`。

### 合成探针

9. 发布含 `SyntheticProbeService` 的二进制（默认关闭，发布本身不改行为）。
10. 用测试用户给每个主力上游分组各开一把 key，`config.yaml` 填 `probe.enabled: true` 与 `probe.targets`（见 `../config.example.yaml`），热升级 / 重启生效。
    2026-11-04 选定（近 7 天用户数 ≥3 的分组，每组取用量最大的便宜模型；新增主力上游时追加）：

    | 分组 | target.model | 7 天请求 / 用户 |
    | --- | --- | --- |
    | 61 DeepSeek-V4.1-Flash-1017 | `deepseek-flash-1017` | 21285 / 104 |
    | 57 自营GLM5.3 | `glm-5.3-flash` | 37252 / 18 |
    | 39 DeepSeek-V4-Flash | `DeepSeek-V4-Flash-0731` | 5118 / 4 |
    | 62 GLM-5.3 | `glm-5.3` | 157 / 4 |
    | 38 MiniMax-M3 | `MiniMax-M3` | 874 / 3 |
11. 验证：`curl -s 127.0.0.1:8080/metrics | grep '^sub2api_probe_'`，每个 target 的 `sub2api_probe_model_ok` 为 1；失败时 `journalctl -u sub2api | grep SyntheticProbe` 有响应体。

## 告警分级

| 级别 | 规则 | 含义 |
| --- | --- | --- |
| critical（发邮件） | `Sub2apiSiteDown` `Sub2apiNotReady` `Sub2apiGatewayPublicDown` | 公网入口 / DB / Redis / 鉴权入口 |
| critical | `Sub2apiUpstreamAuthRejected{platform,model}` | 上游 10 分钟内 ≥3 次 401 / 402 / 403：欠费、密钥失效、封号 |
| critical | `Sub2apiUpstreamAllFailing{platform,model}` | 某模型 15 分钟内 ≥5 次上游失败且 0 次成功 |
| critical | `Sub2apiNoAvailableAccount{platform,model}` | 某模型（不含白名单外 `__other__`）10 分钟 ≥5 次没号可用，**持续 10 分钟** |
| critical | `Sub2apiAccountPoolEmpty` `Sub2apiPostgresDown` `Sub2apiTargetDown` | 可用账号为 0 / 库不可达 / 抓不到且不能用隧道或公网故障解释 |
| critical | `Sub2apiProbeModelFailing{model}` | 合成探针连续两轮失败（含 2xx 但没计量） |
| critical | `Sub2apiUsageNotRecorded` | 15 分钟 ≥20 次成功模型请求但 usage_logs 零新增（计量 / 扣费断） |
| warning（不发邮件） | 原有错误 / 模型级规则（从 page 降级）、`Sub2apiProbeStale`、`Sub2apiSubscriptionExpiryStuck`、`Sub2apiMonitoringTunnelDown`、两个 exporter Down | 降级观察 |

由 metask-ai 规则顺带覆盖、这里不重复：111.229 主机根盘 / 内存（`HostDisk* / HostMemoryLow`）、TLS 证书到期（`MetaskTLSExpiring*` 匹配所有 `blackbox-*` job；这两类告警没有 service 标签，邮件主题是 `[Metask …]`）、`Watchdog` → healthchecks.io 死人开关。

### 阈值依据（2026-11-04 用本机栈 napi 14 天数据回放）

napi 平均 0.53 req/s；7 天上游状态码 200 ×74k、503 ×180、400 ×10、429 ×7，**401/402/403 为 0**。

- `UpstreamAuthRejected`：只在 10-21 11:39–18:50 那次事故响（同时段内部错误、没号、P1 全部飙升）。
- `UpstreamAllFailing`：3 次，10-22 gpt-5.5、10-31 / 11-03 gpt-6-astra，都是真实整线故障。
- `NoAvailableAccount`：原样（tassub2api 版，全局 ≥5 次）会响 436 次——user 7 持续请求 `claude-3-haiku-20240307`（无账号可服务），每天约 2900 次 503，归在 `model="__other__"`。按模型并排除 `__other__` 后 11 次（8 次是 glm-5.2 约 9 分钟的 sglang AIMD 塌缩突发）；`for` 提到 10m 后 3 次。
- `P1ErrorsPresent`（原 page）：219 次，同样被 user 7 的 503 顶在 6%；排除 routing 后 8 次，降为 warning。
- `InternalErrorSpike`（原 page）：137 次，集中在 10-21 事故；平时每天约 40 条（401 Invalid API key、分组订阅限制），降为 warning。
- `AccountPoolEmpty` / `ModelAccountPoolExhausted` / `NoTraffic`：0 次。
- 本机栈每天约 50–85 分钟 `up=0`：是 Mac 经公网抓取的问题（休眠 / 代理），不代表生产；119 经隧道抓取后以 119 的数据为准。

## 与现有监控不冲突

119 上同时有 tassub2api（111.231.4.97，同样的 `sub2api_*` 指标前缀）与 Metask，三处约定：

1. **按 job 区分部署**：本仓库规则只认 `job=~"sub2api|sub2api-backend"`、`pg_up{job="sub2api-postgres"}`；tassub2api 的规则（tassub2api 仓库 `deploy/observability/prometheus.rules.yml`）需同样限定 `job=~"tassub2api|sub2api-backend"`；Metask 的 `MetaskPostgresDown` 需限定 `job=~"metask-postgres|postgres"`。
2. **按 service 分封邮件**：规则与探测目标都带 `service=sub2api`，Alertmanager 按 `[severity, env, service]` 分组，主题前缀 `[sub2api CRITICAL]`（metask-ai 的 `email.tmpl` 已按 service 取前缀）。
3. **抑制规则带 `equal: [service]`**：tassub2api 与 sub2api 共用 `Sub2apiSiteDown → Sub2api(NotReady|GatewayPublicDown|TargetDown)`、`Sub2apiMonitoringTunnelDown → scope=tunnel-*` 等告警名，metask-ai 的 `alertmanager.yml` 里这两条要加 `equal: [service]`，否则一套挂了会压掉另一套的告警。

另：管理后台的「运维告警」（`ops_alert_evaluator_service`，按后台配置的规则发邮件）是进程内的第三套，与本体系独立，不受影响。

## 告警自动分诊（alert-triage）

`119/alert-triage/` 在 Alertmanager 后面加了一层：告警来了先自动取证、按规则表分类，修不了的（欠费 / 密钥失效 / 上游故障 / 没号）带诊断结论发邮件，
代码类的在私有运维仓库建 issue 交给 GitHub Actions 里的 Claude Code，只提 PR、不自动合并发布。原有 `[sub2api CRITICAL]` 邮件保持不变。
上线步骤、规则表写法、回放测试见 `119/alert-triage/README.md`。

## 约定

- 新规则必须带 `severity / env / service=sub2api`，选择器必须带 job；critical 的 description 写第一步排查什么；改完 `docker run --rm -v "$PWD:/w:ro" --entrypoint promtool prom/prometheus:v3.1.0 check rules /w/prometheus.rules.yml`。
- 改 `sub2api.collector.yml` 先在生产只读跑一遍查询，涉及新表 / 新列要先给 `metrics_ro` 授权。
- 指标标签只能是有限集合（不要 user_id / api_key_id / request_id）。
