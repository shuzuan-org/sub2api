# alert-triage：告警自动分诊 + Claude Code 自动修复

接在 119 的 Alertmanager 后面，对 `service=sub2api` 的告警自动取证、分类、分流：

| 去向 | 含义 | 结果 |
| --- | --- | --- |
| `human` | 代码修不了：欠费 / 密钥失效 / 上游故障 / 没号 / 基础设施 | 发邮件 `[sub2api 需人工] …`，带判定、证据、建议 |
| `autofix` | 代码类候选或规则没覆盖 | `notify` 模式发邮件 `[sub2api 代码类候选] …`；`autofix` 模式在私有运维仓库建 issue，由 GitHub Actions 里的 Claude Code 诊断，结论回邮件 `[sub2api 已提PR] …` / `[sub2api AI无法修复] …` |
| `log` | warning 级的上游抖动、客户端鉴权失败等 | 只落库，不发信 |

原有的 `[sub2api CRITICAL]` 邮件不动，本服务挂了也不影响收告警。AI 只提 PR，合并和发布仍由人做。

```
Alertmanager ──webhook──> alert-triage ──┬─ 邮件（human / 结论）
                             │           └─ 私有仓库 issue（auto-fix 标签）──> alert-autofix.yml ──> sub2api 的 PR
                             └─ 取证：Prometheus（必需） + ops_error_logs（可选，triage_ro）
```

## 文件

| 文件 | 说明 |
| --- | --- |
| `triage.py` | 服务本体，只用标准库（`ops_error_logs` 取证才需要 psycopg） |
| `rules.json` | 分类规则表，自上而下第一条命中的生效 |
| `test_triage.py`、`fixtures/replay-2026-11.json` | 单测 + 2026-11 生产上真实触发的 47 次告警及当时的指标证据 |
| `Dockerfile` | 119 上的容器镜像 |
| `ops-repo/alert-autofix.yml` | 放进**私有**运维仓库的工作流（sub2api 是公开仓库，告警证据不能进它的 issue） |
| `../../exporters/triage_ro.sql` | 可选的只读库账号，只能读 `ops_error_logs` 的分类列 |

## 规则表怎么写

每条规则：`alerts`（告警名列表，省略表示任意）、`when`（条件全部成立才命中）、`route`（字符串，或按 severity 分的对象，`default` 兜底）。

| 条件 | 含义 |
| --- | --- |
| `{"share": {"of": "upstream_status", "keys": ["401","402","403"], "exclude": ["200"], "min": 0.5}}` | 某个分布里指定 key 的占比 ≥ min（先去掉 exclude） |
| `{"ratio": {"num": {...}, "den": {...}, "min": 0.6}}` | 跨两个分布的比值，如「上游 429/5xx 次数 ÷ internal+upstream 阶段错误数」 |
| `{"probe_status": [401, 402, 403]}` | 合成探针最近一次的 HTTP 状态码 |
| `{"probe_metered": 0}` | 探针请求的计量是否落库 |

可用的分布：`upstream_status`、`error_phase`、`error_type`（都是告警所在 platform / model 近 30 分钟的计数）。

改规则后跑 `python3 -m unittest -v test_triage`。回放用例里的 `expect` 是当前规则对历史告警的判定，改规则导致判定变化时要逐条确认是不是想要的，再更新 fixture。
对线上某个时刻手工验证：`PROMETHEUS_URL=http://127.0.0.1:9090 python3 triage.py classify '{"alertname":"Sub2apiUpstreamAllFailing","severity":"critical","platform":"openai","model":"glm-5.2"}' <unix时间>`。

2026-11 回放结果：24 次 critical 全部 `human`；23 次 warning 里 21 次 `log`、2 次 `autofix`（11-04、11-06 的内部错误升高，上游全是 200）。

## 配置（环境变量）

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `TRIAGE_MODE` | `notify` | `notify` 只发诊断邮件；`autofix` 才建 issue 触发 AI |
| `TRIAGE_SERVICE` | `sub2api` | 只处理这个 `service` 标签的告警 |
| `PROMETHEUS_URL` | `http://prometheus:9090` | 取证用 |
| `TRIAGE_COOLDOWN_SECONDS` | `21600` | 同一告警（告警名+平台+模型）反复抖动时，多久内不重复发邮件 |
| `TRIAGE_DAILY_AI_CAP` | `5` | 24 小时内最多建几张 AI 工单，超出改发邮件 |
| `SMTP_HOST` / `SMTP_PORT` / `SMTP_USER` / `SMTP_PASSWORD_FILE` / `MAIL_FROM` / `MAIL_TO` | — | 465 隐式 TLS。不配 `SMTP_HOST` 时邮件只打到日志（dry-run） |
| `GITHUB_REPO` / `GITHUB_TOKEN_FILE` | — | 私有运维仓库与机器人 token（issue 读写）；不配则不建 issue |
| `TRIAGE_PG_DSN_FILE` | — | 可选，`triage_ro` 的 DSN；不配就只用 Prometheus 证据 |

## 上线步骤（每一步都会改 119 或生产，执行前确认）

**状态：第一阶段已于 2026-11-13 上线**（119 备份后缀 `docker-compose.yml.bak-20261113T022131Z`、`alertmanager.yml.bak-20261113T022222Z`）。

### 第一阶段：只发诊断邮件（不需要动生产机）

1. 把本目录 scp 到 119 的 `~/server-prometheus/alert-triage/`，在 119 上 `docker build -t alert-triage .`（119 是 x86_64，不要用 Mac 上构建的镜像）。
2. SMTP 密码不用另存：容器以 nobody(65534) 运行，直接只读挂 Alertmanager 的 `alertmanager/secrets/smtp_password`。
3. `~/server-prometheus/docker-compose.yml`（先备份）加：

   ```yaml
   alert-triage:
     image: alert-triage
     container_name: alert-triage
     restart: unless-stopped
     mem_limit: 128m
     environment:
       TRIAGE_MODE: notify
       TRIAGE_SERVICE: sub2api
       PROMETHEUS_URL: http://prometheus:9090
       SMTP_HOST: smtp.feishu.cn
       SMTP_PORT: "465"
       SMTP_USER: <发件账号>
       SMTP_PASSWORD_FILE: /run/secrets/smtp_password
       MAIL_FROM: <发件账号>
       MAIL_TO: <现有收件人>
     volumes:
       - ./alertmanager/secrets/smtp_password:/run/secrets/smtp_password:ro
       - alert-triage-data:/data
   ```

   并在顶层 `volumes:` 加 `alert-triage-data:`。不映射端口，只在 compose 网络内可达。
4. `alertmanager.yml`（先备份；含真实收发件人，只套 diff）：在 `email-critical` 那条路由**之前**加

   ```yaml
   - matchers: [service = sub2api, severity =~ "critical|warning"]
     receiver: triage-webhook
     continue: true
     group_wait: 30s
     group_interval: 5m
     repeat_interval: 4h
   ```

   `receivers:` 加

   ```yaml
   - name: triage-webhook
     webhook_configs:
       - url: http://alert-triage:9099/alert
         send_resolved: true
   ```

   `continue: true` 保证原来的 critical 邮件照发。改之前把候选文件 `docker cp` 进容器跑 `amtool check-config`，通过后**原地覆盖**（`cat 候选 > alertmanager.yml`，保留 inode），再 `docker compose kill -s HUP alertmanager`；
   `amtool config routes test service=sub2api severity=critical env=prod` 应输出 `triage-webhook,email-critical`。
5. 验证：`amtool alert add`（**同时带 `--start` 和 `--end`**）灌一条 `alertname=Sub2apiUpstreamAuthRejected severity=critical env=prod service=sub2api platform=anthropic model=triage-test`
   （用假的 model，免得占掉真实告警的冷却期），应同时收到 `[sub2api CRITICAL] …` 和 `[sub2api 需人工] Sub2apiUpstreamAuthRejected（triage-test）`；`docker compose logs alert-triage` 里有对应一行判定。

观察一到两周，按实际邮件校准 `rules.json`。

### 第二阶段：打开自动修复

6. 建私有仓库，建标签 `alert`、`auto-fix`、`pr-opened`、`needs-human`，把 `ops-repo/alert-autofix.yml` 放到它的 `.github/workflows/`，按文件头注配好 secrets / vars。
   - 仓库所在账号必须能跑私有仓库的 Actions。组织的 Actions 额度用完或扣款失败时任务不会启动
     （`The job was not started because recent account payments have failed…`），可以放到个人账号名下。
   - 模型认证二选一：Claude 订阅 token（`claude setup-token` → `CLAUDE_CODE_OAUTH_TOKEN`）或 API key（`ANTHROPIC_API_KEY`，
     走网关 / 中转时再配 `ANTHROPIC_BASE_URL`、`CLAUDE_MODEL`）。用 API key 时工作流会先探测一次接口，
     失败原因直接写进 issue（secret 填进去就读不回来，这是唯一能看到「key 无效 / 分组下没有账号」的地方）。
   - 需要两个 token：`SUB2API_BOT_TOKEN`（sub2api 的 Contents / Pull requests 读写，放运维仓库的 secret）和
     119 上建 issue 用的 token（运维仓库的 Issues 读写）。细粒度 PAT 只能有一个所有者，两个仓库不在同一账号下时要分开生成。
   - `TRIAGE_BOT_LOGIN` 填 119 那个 token 所属账号的登录名，不必另建机器人账号。
7. 119 上把 token 写到 `~/server-prometheus/alert-triage-secrets/github_token`（属主 65534、权限 0400，目录在镜像构建上下文之外），
   compose 给 alert-triage 加 `TRIAGE_MODE: autofix`、`GITHUB_REPO`、`GITHUB_TOKEN_FILE: /run/secrets/github_token` 和对应的只读挂载后重建容器。
   写 token 时从剪贴板走管道（`pbpaste | ssh … 'cat > …'`），不要把它敲在命令行或聊天里。
8. 验证：灌一条 `alertname=Sub2apiUsageNotRecorded` → 私有仓库出现 issue → 工作流运行 → issue 得到 `pr-opened` 或 `needs-human` → 2 分钟内收到结论邮件。
   要验证「提 PR」这条路：往 sub2api 推一个埋了 bug 的临时分支，把运维仓库的 variable `SUB2API_BASE_REF` 指向它，手工建一张带证据的 issue 并打 `auto-fix`；
   测完关掉 PR、删分支、删这个 variable。

2026-11-13 已按以上步骤上线并实测：欠费场景 → `needs-human`；埋 bug → AI 改 1 行 + 补测试 + 提 PR（约 15 分钟，主要是跑单元测试）；
119 自动建单 → AI 判 `needs-human` → 结论邮件并关单（约 2 分钟）。「119 自动建单一路到 `已提PR` 邮件」没有连起来跑过。

### 可选：ops_error_logs 证据

9. 111.229 上执行 `exporters/triage_ro.sql`；`authorized_keys` 里 119 的 `tunnel_key` 追加 `permitopen="127.0.0.1:5432"`；
   119 的 `tunnel-sub2api-11122923575.service` 加 `-L 172.17.0.1:9114:127.0.0.1:5432`；
   `secrets/pg_dsn` 写 `postgresql://triage_ro:<密码>@172.17.0.1:9114/sub2api_v2`，compose 加 `TRIAGE_PG_DSN_FILE`。
   这一段代码（`pg_evidence`）还没有对真实库跑过，接通后先看日志确认。

## 已知限制

- sub2api 仓库目前没有 CI 在跑，AI 提的 PR 只在它自己的运行环境里跑过 `make -C backend test-unit`，合并前要人看改动。
- 用 Claude 订阅 token 时，AI 的运行占用该订阅的用量；token 有效期一年，到期要重新生成。
- PR 描述由 AI 生成并出现在公开仓库。提示词要求不带生产数据，但没有机器校验，合并前留意。
- AI 的改动范围被限制在后端非迁移、非生成代码；前端和迁移一律转人工。
- 只处理 `service=sub2api`。tassub2api / metask-ai 接入时各起一个实例（`TRIAGE_SERVICE` 不同）并补各自的规则表。
