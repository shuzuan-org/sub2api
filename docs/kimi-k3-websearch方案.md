# Kimi K3 + WebSearch 接入方案

> 目标：sub2api 网关上可访问 Kimi K3，且该模型具备 websearch 能力。
> 调查日期：2026-07-28。所有结论均已在生产环境实测验证。
> **状态：已于 2026-07-28 10:44–10:48 实施完成并验收通过**（见文末「执行记录」）。

## 结论先行

**K3 已经接进来了，缺的只是 websearch。** 让它有 websearch 只需要两处配置改动，**不需要改任何代码、不需要部署二进制**。

## 现状（已实测）

### sub2api 侧

生产库 `sub2api_v2`：

| account | name | platform | base_url | model_mapping |
|---|---|---|---|---|
| 71 | `yuguan-kimi-k3` | anthropic/apikey | `https://zhongzhuan.remarkablecue.com`（中转） | `claude-opus-4-6/4-7/4-8` → `kimi-k3` |

- 归属 group 46 `yuguan-kimi-k3`（该组只有这一个账号），创建于 2026-07-27。
- 至今只跑过 1 次请求（`usage_logs` id=2062696）。
- **计费正常**：`pricing_snapshot = {input:0.00021, output:0.00105, cache_read:0.000021, source:"litellm"}`，换算 = $3 / $15 / $0.30 每 MTok，与 Moonshot 官方 K3 标价完全一致。LiteLLM 已收录 `kimi-k3`，**定价侧无需任何改动**。

对比：H200 / paratera / deepseek 系账号的 base_url 都是 `http://127.0.0.1:8188`（即 sglang-proxy）。

**只有 K3 这条线是直连中转、绕过 sglang-proxy 的 —— 这就是它没有 websearch 的唯一原因。**

### websearch 究竟在哪一层

不在 sub2api，在 **sglang-proxy**（`/Users/warriorzhu/code/meta-task/sglang-proxy`，生产跑在 `111.229.235.75:8188`）。它有两套彼此独立的 websearch 实现：

| # | 路径 | 机制 | 与模型/dialect 的关系 |
|---|---|---|---|
| 1 | `/v1/messages`（Anthropic，sub2api 主链路） | Claude Code 发独立的 web_search 子请求 → `hasWebSearch()` 识别 → 整个请求改写 model 后转发 antiapi(cc2codex) | **与 dialect 无关**，任何上游都生效 |
| 2 | `/v1/chat/completions`、`/v1/responses` | hosted loop：模型自己调 `WebSearch` 工具，proxy 拦截→执行→喂回→续跑 | **只有 GLM 实现了拦截器**（`model_glm_stream.go:106`） |

我们要用的是第 1 套。关键代码事实（`sglang-proxy/main.go`）：

- `main.go:1733-1822` — web_search 分流；
- `main.go:1830` 起才是 passthrough 分流，注释原文：*"web_search above already took priority and went to antiapi regardless"*；
- `main.go:1860` 的上下文超限拒绝（`rejectIfContextExceedsServing`）**在 passthrough 分支 return 之后**，注释原文：*"Passthrough requests skip this"* —— 所以 K3 的 1M 上下文不会被本地 GLM-5.2 的窗口（实测 262144）误伤。

生产 `.env` 中 websearch 后端：`ANTIAPI_URL=https://lisa.vspeak.top`，`WEB_SEARCH_MODEL=claude-sonnet-4-6`。

**机制已在生产验证**：近 7 天 journalctl 中 `→ antiapi (web_search sub-request)` 出现 **3254 次**，持续在跑。

## 方案

把 account 71 挪到 sglang-proxy 后面，中转变成 proxy 的 passthrough 上游。

```
改前：CC ──> sub2api ──────────────────────────> 中转 ──> K3      （无 websearch）

改后：CC ──> sub2api ──> sglang-proxy:8188 ──┬─> 中转 ──> K3
                                              └─> antiapi(cc2codex) ──> websearch
```

### 改动 1：sglang-proxy `.env`（`/home/david/sglang-proxy/.env`）

`ROUTES` 数组追加一条（JSON 单行，不加外层引号）：

```json
{"token":"yuguan-kimi-k3","type":"passthrough","url":"https://zhongzhuan.remarkablecue.com","key":"<account 71 现有的 api_key>","model":""}
```

- `model:""` = 透传客户端 model。sub2api 已经在自己这层把 `claude-opus-4-8` 改写成 `kimi-k3` 了（`usage_logs.upstream_model=kimi-k3` 可证），proxy 不要再改。
- 改完 `sudo systemctl restart sglang-proxy` 生效。

> ⚠️ **必须 restart，`reload` 不行。** `dotenvcfg.Load` 只在 `os.LookupEnv(k)` 不存在时才写入
> （`internal/dotenvcfg/dotenv.go:50`）。tableflip 热升级是 fork+exec，子进程继承父进程环境，
> 而父进程启动时已经 `os.Setenv("ROUTES", 旧值)` —— 于是子进程认为 ROUTES 已存在，直接跳过
> .env 里的新值。**reload 会静默地用旧配置起新进程**，比报错更难查。
>
> restart 的代价：SIGTERM → 关闭监听 → `srv.Shutdown(drainTimeout=2h)`，被 unit 的
> `TimeoutStopSec=1800` 截断。即 8188 端口在在途请求排空前一直拒连，最坏 30 分钟，
> 期间所有指向 `127.0.0.1:8188` 的账号全部不可用。
> **做法：先轮询 `/debug/vars` 的 `proxy_requests_active`，等它为 0 的瞬间再 restart。**
> 本次实测这样操作耗时 1 秒。

### 改动 2：sub2api account 71

```sql
UPDATE accounts SET
  credentials = jsonb_set(
    jsonb_set(credentials, '{base_url}', '"http://127.0.0.1:8188"'),
    '{api_key}', '"yuguan-kimi-k3"'),
  updated_at = now()
WHERE id = 71;
```

`model_mapping` 保持不动。

**改完必须清 Redis 快照**：`redis-cli -n 0 del sched:acc:71`。

账号快照存在 Redis 的 `sched:acc:<id>`，`SetAccount` 写入时 TTL 传的是 `0`（永不过期，
`internal/repository/scheduler_cache.go:159`），裸改数据库不会同步。删掉该 key 是安全的：
`GetSnapshot` 在 MGET 发现任一账号 key 缺失时返回 `(nil,false,nil)` 当作缓存未命中
（`scheduler_cache.go:76-78`），整桶回退查库并重建；单账号读也有 `accountRepo.GetByID` 兜底
（`scheduler_snapshot_service.go:152`）。

### 为什么不需要动代码

- 线上二进制是 **2026-07-06 构建**（commit `9ebe796`），之后 master 上堆了 20+ 个未发布提交（metering / controlplane / MetaGate / routes v2）。从 HEAD 构建部署 = 把这一整坨一起上线，风险不对等。
- 本方案只用到 `{token,type,url,key,model}` 这 5 个 ROUTES 字段，7-06 的二进制完全支持。**零部署。**

## 已知副作用（可接受，非阻塞）

passthrough 路由的 `/v1/models` **只透出内嵌 snapshot（`internal/modelcatalog/snapshot.json`）里定义过的 model id**。

实测佐证：paratera 上游有 98 个模型，经 proxy 后只剩 5 个（`MiniMax-M2.7 / GLM-5.2 / MiniMax-M3 / MiniMax-M2 / DeepSeek-V4-Pro`）；上游确实有 `Kimi-K2.5`、`Kimi-K2.6`，因不在 snapshot 被过滤掉。

snapshot 当前最新的 Kimi 是 `kimi-k2.7`，**没有 `kimi-k3`**。所以改完之后，该 token 的 `/v1/models` 会返回空列表。

**但这不构成回归**：中转现在返回的 kimi-k3 条目本身就没有 `context` / `max_tokens` 字段，sub2api 拿到的 `max_input_tokens` 今天已经是 0。而客户端实际请求的是 `claude-opus-4-*` 别名，走 superset 的 Claude 家族兜底窗口，不受影响。

若要彻底修好（让 `/v1/models` 正确申报 K3 的 1M 窗口），需要往 snapshot 加一条：

```json
{"id":"kimi-k3","display_name":"Kimi K3","context":1000000,"max_output":<待确认>,
 "release_date":"2026-07-16","reasoning":true,"attachment":true,
 "structured_output":true,"input_image":true,"input_video":false}
```

建议做法：**从 `9ebe796` 拉分支，只加这一条，构建部署**，不要从 master HEAD 发。这一步可以和主方案解耦、之后再做。

## K3 事实核对（2026-07 公开信息）

- 2026-07-16 Moonshot 上线 K3 API；2026-07-26 公开权重。
- 2.8T 总参数，1M 上下文，原生视觉，always-on thinking。
- 定价 $0.30（cache hit）/ $3.00（cache miss）/ $15.00 输出，每 MTok —— 与库里 litellm 快照一致。
- 官方 Anthropic 兼容端点：`https://api.moonshot.ai/anthropic`（国际）/ `https://api.moonshot.cn/anthropic`（国内）。如果哪天想绕开中转直连官方，把改动 1 的 `url` 换成它即可，方案其余部分不变。
- **自建不可行**：2.8T 参数即使 W4 量化也约 1.4TB，超过 H200 单机 8×143GB=1144GB 显存。所以 K3 只能走 passthrough，不存在"sglang 本地跑 K3"这条路 —— 也因此不需要写 K3 dialect（sglang-proxy 里只有 `KimiK2Dialect`，K3 的 chat template 未验证）。

## 执行记录（2026-07-28）

| 时间 | 动作 | 结果 |
|---|---|---|
| 10:43 | 备份 `.env` → `.env.bak-20260728-kimik3`，ROUTES 追加 `yuguan-kimi-k3` passthrough 条目 | tokens: `shuzuan2025-minimax`, `paratera`, `yuguan-kimi-k3` |
| 10:44:39 | 轮询到 `proxy_requests_active=0`，`systemctl restart sglang-proxy` | **耗时 1 秒**；日志 `routes mode: 3 routes (2 passthrough proxies)` → `ready` |
| 10:44:58 | 直连代理冒烟：`POST :8188/v1/messages` model=kimi-k3 | 返回 `PONG`，`→ passthrough model=kimi-k3→kimi-k3` |
| 10:45:12 | 直连代理 websearch：带 `web_search_20250305` 工具 | `→ antiapi (web_search sub-request) model=kimi-k3→claude-sonnet-4-6`，返回真实带引用的 `web_search_tool_result` |
| 10:46 | `UPDATE accounts ... WHERE id=71` + `redis-cli del sched:acc:71` | base_url → `http://127.0.0.1:8188`，api_key → `yuguan-kimi-k3` |
| 10:47:43 | 端到端：临时 key → sub2api:8080 → 代理 → K3，model=`claude-opus-4-8` | 返回 `PONG`，`usage_logs.upstream_model=kimi-k3` |
| 10:48:08 | 端到端 websearch | 收到 `server_tool_use` ×1 + `web_search_tool_result` ×1 + `text_delta` ×25 |
| 10:48 | 删除临时验证 key（api_keys id=546） | 已清理 |

**计费核对**：新产生的 4 条 `usage_logs` 全部 `pricing_snapshot.source=litellm`，
`upstream_model=kimi-k3`，单价 $3 / $15 / $0.30 每 MTok，与官方一致。

**真实流量已在新链路上**：10:47 期间 api_key_id=160（user 61）的请求已经走通新路径并正常计费，
说明切换对在用客户无感。

## 验收 / 回滚

1. `journalctl -u sglang-proxy -f | grep -E 'passthrough|web_search'`，用 group 46 的 key 发一次带 WebSearch 的对话；
2. 应看到 `→ passthrough model=kimi-k3→kimi-k3`，以及模型触发搜索时的 `→ antiapi (web_search sub-request) model=kimi-k3→claude-sonnet-4-6`；
3. sub2api `usage_logs` 中 account_id=71 新增记录，`upstream_model=kimi-k3`，`pricing_snapshot.source=litellm`；
4. 回滚：`sudo cp /home/david/sglang-proxy/.env.bak-20260728-kimik3 /home/david/sglang-proxy/.env` + 等 idle 后 restart；account 71 的 base_url/api_key 改回中转并 `del sched:acc:71`。两边都是纯配置，秒回。
