#!/usr/bin/env bash
# sub2api 生产部署脚本
# 用法: ./deploy.sh [--skip-build] [--skip-frontend] [--no-backup]
#
# 部署策略：
#   1. 本地 build（可选：跳过前端打包 or 跳过整个编译）
#   2. 备份远端现有二进制
#   3. 上传新二进制（先到 /tmp，再 sudo mv 到目标目录）
#   4. 重启服务
#   5. 健康检查（最多等 30 秒），失败自动回滚
#
# 关键约束：
#   - Claude Code 本身通过这台机器调用服务端 API，部署失败会直接断掉自己的链路
#   - 停止服务后必须等待新进程真正 listening 才算部署完成
#   - 失败时自动回滚到 backup 二进制

set -euo pipefail

# ── 配置 ──────────────────────────────────────────────────────────────────────
# 唯一部署目标。需要临时发到别的机器时用环境变量覆盖：REMOTE_HOST=ubuntu@x.x.x.x ./deploy.sh
REMOTE_HOST="${REMOTE_HOST:-ubuntu@111.229.235.75}"
REMOTE_BIN="/opt/sub2api/sub2api"
REMOTE_BACKUP="/opt/sub2api/sub2api.backup"
REMOTE_SERVICE="sub2api"
HEALTH_URL="http://127.0.0.1:8080/health"
# 自动检测项目根目录（兼容 macOS 和 WSL/Linux）。本脚本位于 <root>/deploy/。
_SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
_PROJECT_ROOT="$(cd "${_SCRIPT_DIR}/.." && pwd)"
LOCAL_BIN="${LOCAL_BIN:-${_PROJECT_ROOT}/backend/bin/server}"
FRONTEND_DIR="${FRONTEND_DIR:-${_PROJECT_ROOT}/frontend}"
BACKEND_DIR="${BACKEND_DIR:-${_PROJECT_ROOT}/backend}"

SKIP_BUILD=false
SKIP_FRONTEND=false
NO_BACKUP=false
for arg in "$@"; do
  case $arg in
    --skip-build)    SKIP_BUILD=true    ;;
    --skip-frontend) SKIP_FRONTEND=true ;;
    --no-backup)     NO_BACKUP=true     ;;
  esac
done

# ── 颜色输出 ──────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
info()    { echo -e "${GREEN}[deploy]${NC} $*"; }
warn()    { echo -e "${YELLOW}[warn]${NC}  $*"; }
err()     { echo -e "${RED}[error]${NC} $*" >&2; }

# 先把目标喊出来，避免用环境变量覆盖后发错机器。
info "目标机：${REMOTE_HOST}"

# ── 健康检查 ──────────────────────────────────────────────────────────────────
wait_healthy() {
  local max_wait=30
  local interval=2
  local elapsed=0
  info "等待服务就绪（最多 ${max_wait}s）..."
  while [ $elapsed -lt $max_wait ]; do
    sleep $interval
    elapsed=$((elapsed + interval))
    if ssh "$REMOTE_HOST" "curl -sf --max-time 3 '$HEALTH_URL' >/dev/null"; then
      info "服务健康检查通过 (${elapsed}s)"
      return 0
    fi
    echo -n "."
  done
  echo ""
  err "健康检查超时（${max_wait}s），服务未就绪"
  return 1
}

# ── 前端嵌入校验（上传前）────────────────────────────────────────────────────
# /health 是纯后端端点，前端没嵌进二进制时它照样 200，健康检查拦不住白屏。
# 前端走 go:embed + build tag（backend/internal/web/embed_on.go 的 //go:build embed），
# 漏了 -tags embed 编出来的二进制里 SPA 路由压根不注册，"/" 会被 gin 默认 NoRoute
# 接走返回 18 字节的 "404 page not found" —— 表现就是首页白屏，而所有 API 全正常。
# 这里直接在二进制里找标记位，比信任操作员记得加 flag 可靠。
verify_embed() {
  info "校验二进制是否嵌入前端..."
  # 用正向标记判断：go:embed all:dist 会把 dist/ 下的文件路径原样写进二进制。
  # 实测——嵌入版含 dist/index.html 与几十条 dist/assets/*；非嵌入版两者都是 0。
  # 注：别改用 embed_off.go 里那句 "Frontend not embedded" 当反向指纹，
  # 那段代码在非嵌入构建里会被链接器 DCE 掉，字符串根本不落盘（实测命中 0 次）。
  if grep -qa "dist/index.html" "$LOCAL_BIN" && grep -qa "dist/assets/" "$LOCAL_BIN"; then
    info "前端嵌入校验通过"
    return 0
  fi
  err "二进制里找不到嵌入的前端资源，部署上去首页必然白屏"
  err "（API 和 /health 仍然全部正常，健康检查拦不住，只有人去点首页才会发现）。"
  err "正确编法：cd ${BACKEND_DIR} && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \\"
  err "            go build -tags embed -ldflags=\"-s -w -X main.Version=\$(cat cmd/server/VERSION)\" -trimpath -o bin/server ./cmd/server"
  err "或者：去掉 --skip-build 重跑本脚本（脚本自带的编译命令是对的）。"
  err "如果确认加了 -tags embed 仍报此错，检查 ${FRONTEND_DIR} 是否已构建（产物应在 backend/internal/web/dist/）。"
  exit 1
}

# ── 首页可用性检查（部署后）──────────────────────────────────────────────────
# 兜底防线：万一上面的指纹检查被绕过（比如换了构建方式），这里从真实响应上再确认一次。
check_frontend_serving() {
  info "验证首页返回 HTML..."
  local body
  body=$(ssh "$REMOTE_HOST" "curl -sf --max-time 5 'http://127.0.0.1:8080/' 2>/dev/null | head -c 200" 2>/dev/null || true)
  if printf '%s' "$body" | grep -qi '<!doctype html'; then
    info "首页正常"
    return 0
  fi
  err "首页未返回 HTML，前端资源缺失。实际响应开头：${body:-<空>}"
  return 1
}

# ── 回滚 ──────────────────────────────────────────────────────────────────────
rollback() {
  set +e  # 回滚路径不能被 set -e 截断，必须跑完所有步骤
  err "部署失败，执行回滚..."
  ssh "$REMOTE_HOST" "
    if [ -f '${REMOTE_BACKUP}' ]; then
      # rename-safe 还原：旧进程可能仍在运行，cp 直接覆盖会 ETXTBSY；先 cp 到临时再 mv 原子替换。
      sudo cp '${REMOTE_BACKUP}' '${REMOTE_BIN}.rollback'
      sudo chown sub2api:sub2api '${REMOTE_BIN}.rollback'
      sudo chmod +x '${REMOTE_BIN}.rollback'
      sudo mv -f '${REMOTE_BIN}.rollback' '${REMOTE_BIN}'
      sudo systemctl restart '${REMOTE_SERVICE}'
      echo 'rollback: 已恢复 backup 并重启服务'
    else
      echo 'rollback: 无 backup 文件，尝试重启现有服务'
      sudo systemctl restart '${REMOTE_SERVICE}' || true
    fi
  "
  if wait_healthy; then
    warn "回滚成功，服务已恢复"
  else
    err "回滚后服务仍不健康，需要人工介入"
    err "SSH 进入：ssh ${REMOTE_HOST}"
    err "查日志：journalctl -u ${REMOTE_SERVICE} -n 50"
  fi
  exit 1
}

# ── 步骤 1：本地 build ────────────────────────────────────────────────────────
if [ "$SKIP_BUILD" = true ]; then
  info "跳过编译（--skip-build）"
  warn "将直接上传已有的 ${LOCAL_BIN}（下面会校验它是否嵌入了前端）"
else
  if [ "$SKIP_FRONTEND" = false ]; then
    info "打包前端..."
    cd "$FRONTEND_DIR"
    if command -v pnpm >/dev/null 2>&1 && [ -f pnpm-lock.yaml ]; then
      pnpm build
    else
      npm run build
    fi
    info "前端打包完成"
  else
    warn "跳过前端打包（--skip-frontend）：将嵌入 backend/internal/web/dist 里的现存产物，可能是旧版前端"
  fi

  info "编译 linux/amd64 二进制（嵌入前端）..."
  cd "$BACKEND_DIR"
  # 打构建标识：进程启动时会把这几个值写进日志，事后能直接反查跑的是哪次构建。
  # 工作区脏就加 -dirty 后缀，避免 commit 号指向一个和实际产物不一致的快照。
  BUILD_COMMIT="$(git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)"
  if [ -n "$(git status --porcelain 2>/dev/null)" ]; then
    BUILD_COMMIT="${BUILD_COMMIT}-dirty"
  fi
  BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
    go build -tags embed \
    -ldflags="-s -w -X main.Version=$(cat cmd/server/VERSION) -X main.Commit=${BUILD_COMMIT} -X main.Date=${BUILD_DATE}" \
    -trimpath -o bin/server ./cmd/server
  info "编译完成：$(ls -lh bin/server | awk '{print $5}')（commit: ${BUILD_COMMIT}）"
fi

if [ ! -f "$LOCAL_BIN" ]; then
  err "二进制不存在：$LOCAL_BIN"
  exit 1
fi

verify_embed

# ── 步骤 2：部署前健康检查（记录当前状态）────────────────────────────────────
info "部署前检查当前服务状态..."
# 仅记录，不阻断——即使服务当前 down 也允许强制部署（用于修复性发布）
if ssh "$REMOTE_HOST" "curl -sf --max-time 5 '$HEALTH_URL' >/dev/null"; then
  info "部署前服务状态：ok"
else
  warn "部署前服务状态：down（继续部署）"
fi

# ── 步骤 3：备份远端二进制 ────────────────────────────────────────────────────
if [ "$NO_BACKUP" = false ]; then
  info "备份远端二进制..."
  ssh "$REMOTE_HOST" "sudo cp '${REMOTE_BIN}' '${REMOTE_BACKUP}'" || warn "备份失败（可能首次部署），继续..."
fi

# ── 步骤 4：上传新二进制 ──────────────────────────────────────────────────────
info "上传二进制到 ${REMOTE_HOST}:${REMOTE_BIN}..."
# 先上传到 /tmp（ubuntu 可写），再 sudo mv 到目标目录
# mv 是原子操作，不会影响正在运行的进程（内核保持旧 inode 直到进程退出）
# 用 rsync 替代 scp：支持断点续传，中途断连重试即可，不会从头开始
rsync -az --progress "$LOCAL_BIN" "${REMOTE_HOST}:/tmp/sub2api.new_upload"
ssh "$REMOTE_HOST" "
  chmod +x /tmp/sub2api.new_upload
  sudo mv /tmp/sub2api.new_upload '${REMOTE_BIN}.new_upload'
  sudo chown sub2api:sub2api '${REMOTE_BIN}.new_upload'
"

# ── 步骤 5：原子替换 → 零停机热升级（tableflip reload）────────────────────────
# mv(rename) 可原子替换正在运行的二进制：运行中的进程保留旧 inode 不受影响
# （ETXTBSY「Text file busy」只发生在 cp/写入打开的可执行文件，rename 不会）。
# 随后 `systemctl reload`(SIGHUP) 触发进程内 tableflip fork+exec 磁盘上的新二进制并继承监听 fd，
# 端口无空窗、在途长流被排空而非硬杀。通过 MainPID 是否变化判定交接是否成功（防 stale binary 静默通过）。
info "原子替换二进制并热升级（零停机 reload）..."
ssh "$REMOTE_HOST" "
  set -e
  OLD_PID=\$(systemctl show -p MainPID --value '${REMOTE_SERVICE}')
  sudo mv -f '${REMOTE_BIN}.new_upload' '${REMOTE_BIN}'
  sudo systemctl reload '${REMOTE_SERVICE}'
  # 等待 tableflip 完成交接：MainPID 必须从 OLD_PID 变为新值
  for i in \$(seq 1 30); do
    NEW_PID=\$(systemctl show -p MainPID --value '${REMOTE_SERVICE}')
    if [ -n \"\$NEW_PID\" ] && [ \"\$NEW_PID\" != \"0\" ] && [ \"\$NEW_PID\" != \"\$OLD_PID\" ]; then
      echo \"热升级成功：MainPID \$OLD_PID -> \$NEW_PID\"
      exit 0
    fi
    sleep 1
  done
  echo \"热升级失败：MainPID 未变化（仍为 \$OLD_PID），新二进制可能启动失败被 tableflip 中止\" >&2
  exit 1
" || { err "热升级未完成交接"; rollback; }

# ── 步骤 5.5：清除计费缓存（迁移后一次性操作）────────────────────────────────
# 金额单位迁移（USD→U）后 Redis 中可能残留旧单位的余额/订阅缓存。
# 这些 key TTL 仅 5 分钟，但在 TTL 内会导致读到脏数据。
# 此操作幂等安全——已清或无 key 时无副作用。
info "清除计费缓存..."
ssh "$REMOTE_HOST" "
  for prefix in 'billing:balance:' 'billing:sub:' 'apikey:rate:'; do
    count=\$(redis-cli --scan --pattern \"\${prefix}*\" | wc -l)
    if [ \"\$count\" -gt 0 ]; then
      redis-cli --scan --pattern \"\${prefix}*\" | xargs redis-cli DEL >/dev/null 2>&1
      echo \"  清除 \${prefix}* : \${count} keys\"
    fi
  done
" || warn "Redis 缓存清除失败（非致命，缓存会在 5 分钟内自然过期）"

# ── 步骤 6：等待服务就绪（关键：Claude Code 依赖此服务）────────────────────────
if ! wait_healthy; then
  rollback
fi

# 健康检查只覆盖后端，首页得单独确认一次，否则白屏会静默通过部署。
if ! check_frontend_serving; then
  rollback
fi

# ── 完成 ──────────────────────────────────────────────────────────────────────
VERSION=$(cat "$BACKEND_DIR/cmd/server/VERSION" 2>/dev/null || echo "unknown")
info "✓ 部署成功！版本：$VERSION"
info "日志：ssh ${REMOTE_HOST} 'journalctl -u ${REMOTE_SERVICE} -n 20'"
