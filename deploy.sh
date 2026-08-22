#!/bin/bash
# ============================================================
# 宿舍电费监控 — Linux 一键部署脚本
# 用法:
#   curl -sL https://github.com/mico-v/wxxyshall_monitoring/releases/latest/download/deploy.sh | bash
# 或:
#   wget -qO- https://github.com/mico-v/wxxyshall_monitoring/releases/latest/download/deploy.sh | bash
# ============================================================
set -euo pipefail

# ---- 配置 ----
REPO="mico-v/wxxyshall_monitoring"
INSTALL_DIR="/opt/elec-monitor"
DATA_DIR="${INSTALL_DIR}/data"
SERVICE_NAME="elec-monitor"
GITHUB="https://github.com/${REPO}"
API="https://api.github.com/repos/${REPO}/releases/latest"

# ---- 颜色 ----
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log()  { echo -e "${GREEN}[✓]${NC} $1"; }
warn() { echo -e "${YELLOW}[!]${NC} $1"; }
err()  { echo -e "${RED}[✗]${NC} $1"; exit 1; }

# ---- 检查 root ----
if [ "$(id -u)" -ne 0 ]; then
  err "请以 root 运行: sudo bash deploy.sh"
fi

# ---- 检测系统架构 ----
ARCH=$(uname -m)
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  *)       err "不支持的架构: $ARCH (仅支持 amd64/arm64)" ;;
esac

log "系统架构: linux-${ARCH}"

# ---- 安装依赖 ----
install_deps() {
  if command -v apt-get &>/dev/null; then
    apt-get update -qq && apt-get install -y -qq curl jq 2>/dev/null
  elif command -v yum &>/dev/null; then
    yum install -y -q curl jq 2>/dev/null
  elif command -v apk &>/dev/null; then
    apk add --no-cache curl jq 2>/dev/null
  fi
}

# ---- 下载最新 release ----
download_release() {
  log "获取最新版本信息..."
  local LATEST
  LATEST=$(curl -sL "$API" | jq -r '.tag_name // empty')
  if [ -z "$LATEST" ]; then
    warn "无法获取最新版本，将使用 latest 标签"
    LATEST="latest"
  fi
  log "最新版本: ${LATEST}"

  local URL="${GITHUB}/releases/download/${LATEST}/monitor-linux-${ARCH}.tar.gz"
  log "下载: ${URL}"

  mkdir -p "${INSTALL_DIR}"
  curl -sL "$URL" | tar xz -C "${INSTALL_DIR}"
  chmod +x "${INSTALL_DIR}/monitor" "${INSTALL_DIR}/webapp"

  # 下载前端资源
  log "下载前端资源..."
  for f in webapp.html 404.html sw.js manifest.json offline.html config.example.json; do
    curl -sL "${GITHUB}/raw/main/${f}" -o "${INSTALL_DIR}/${f}" 2>/dev/null || true
  done
  mkdir -p "${INSTALL_DIR}/static"
  curl -sL "${GITHUB}/raw/main/static/echarts.min.js" -o "${INSTALL_DIR}/static/echarts.min.js" 2>/dev/null || true

  log "二进制文件已安装到 ${INSTALL_DIR}"
}

# ---- 创建数据目录和 ADMIN_KEY ----
setup_data_dir() {
  mkdir -p "${DATA_DIR}"

  # 首次部署：从示例创建默认 config
  if [ ! -f "${DATA_DIR}/config.json" ]; then
    cp "${INSTALL_DIR}/config.example.json" "${DATA_DIR}/config.json"
    warn "默认配置文件已创建: ${DATA_DIR}/config.json"
    warn "请编辑该文件填写学号，或通过网页「查询设置」添加宿舍"
  fi

  # 生成 ADMIN_KEY（持久化到 .admin_key，重启不变）
  KEY_FILE="${DATA_DIR}/.admin_key"
  if [ ! -s "$KEY_FILE" ]; then
    ADMIN_KEY="$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')"
    printf '%s\n' "$ADMIN_KEY" > "$KEY_FILE"
    chmod 600 "$KEY_FILE"
  else
    ADMIN_KEY="$(cat "$KEY_FILE")"
  fi
  export ADMIN_KEY

  # 把 ADMIN_KEY 写入 config.json 的 environment 字段
  # 如果 config.json 还没有 admin_key 字段，追加
  if ! grep -q '"admin_key"' "${DATA_DIR}/config.json" 2>/dev/null; then
    # 在 config.json 的 username 行后插入 admin_key
    sed -i "s/\"username\":.*/\"admin_key\": \"${ADMIN_KEY}\",\n  \0/" "${DATA_DIR}/config.json" 2>/dev/null || true
  fi

  chmod 755 "${DATA_DIR}"
}

# ---- 创建 systemd 服务 ----
setup_systemd() {
  cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<EOF
[Unit]
Description=宿舍电费监控 - 采集 + 仪表盘
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=${INSTALL_DIR}
Environment="ADMIN_KEY=${ADMIN_KEY}"
	Environment="USTS_DATA_DIR=${DATA_DIR}"
Environment="TZ=Asia/Shanghai"
ExecStartPre=${INSTALL_DIR}/monitor --config-dir ${DATA_DIR}
ExecStart=${INSTALL_DIR}/webapp --host 0.0.0.0 --port 8080 --config-dir ${DATA_DIR}
Restart=always
RestartSec=30
# 日志
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  log "systemd 服务已创建: ${SERVICE_NAME}"
}

# ---- 创建监控定时器（monitor 采集循环） ----
setup_timer() {
  cat > "/etc/systemd/system/${SERVICE_NAME}-collect.service" <<EOF
[Unit]
Description=宿舍电费监控 - 采集任务
After=network.target

[Service]
Type=oneshot
User=root
WorkingDirectory=${INSTALL_DIR}
Environment="ADMIN_KEY=${ADMIN_KEY}"
	Environment="USTS_DATA_DIR=${DATA_DIR}"
Environment="TZ=Asia/Shanghai"
ExecStart=${INSTALL_DIR}/monitor --config-dir ${DATA_DIR}
EOF

  cat > "/etc/systemd/system/${SERVICE_NAME}-collect.timer" <<EOF
[Unit]
Description=宿舍电费监控 - 定时采集（每 60 分钟）
Requires=${SERVICE_NAME}-collect.service

[Timer]
OnBootSec=2min
OnUnitActiveSec=60min
Unit=${SERVICE_NAME}-collect.service

[Install]
WantedBy=timers.target
EOF

  systemctl daemon-reload
  systemctl enable "${SERVICE_NAME}-collect.timer" 2>/dev/null || true
  systemctl start "${SERVICE_NAME}-collect.timer" 2>/dev/null || true
  log "定时采集已启用（每 60 分钟）"
}

# ---- 启动服务 ----
start_service() {
  systemctl enable "${SERVICE_NAME}" 2>/dev/null
  systemctl restart "${SERVICE_NAME}"
  log "服务已启动: http://$(curl -s ifconfig.me 2>/dev/null || hostname -I | awk '{print $1}'):8080"

  sleep 2
  if systemctl is-active --quiet "${SERVICE_NAME}"; then
    log "服务运行状态: $(systemctl is-active ${SERVICE_NAME})"
  else
    warn "服务启动失败，查看日志: journalctl -u ${SERVICE_NAME} -n 50"
  fi
}

# ---- 创建自动更新脚本 ----
setup_autoupdate() {
  cat > "${INSTALL_DIR}/update.sh" <<'UPDATEEOF'
#!/bin/bash
# 自动更新脚本
set -euo pipefail

REPO="mico-v/wxxyshall_monitoring"
INSTALL_DIR="/opt/elec-monitor"
DATA_DIR="${INSTALL_DIR}/data"
API="https://api.github.com/repos/${REPO}/releases/latest"
VERSION_FILE="${INSTALL_DIR}/.version"

# 获取当前版本
CURRENT=""
if [ -f "$VERSION_FILE" ]; then
  CURRENT=$(cat "$VERSION_FILE")
fi

# 获取最新版本
LATEST=$(curl -sL "$API" | jq -r '.tag_name // empty')
if [ -z "$LATEST" ]; then
  echo "[!] 无法获取最新版本"
  exit 1
fi

if [ "$CURRENT" = "$LATEST" ]; then
  echo "[✓] 已是最新版本: $LATEST"
  exit 0
fi

echo "[*] 发现新版本: $CURRENT → $LATEST"

ARCH=$(uname -m)
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  *)       echo "[!] 不支持的架构"; exit 1 ;;
esac

# 下载新版本
URL="https://github.com/${REPO}/releases/download/${LATEST}/monitor-linux-${ARCH}.tar.gz"
TMP=$(mktemp -d)
curl -sL "$URL" | tar xz -C "$TMP"

# 停止服务
systemctl stop elec-monitor 2>/dev/null || true

# 替换二进制
cp "$TMP/monitor" "$INSTALL_DIR/monitor"
cp "$TMP/webapp" "$INSTALL_DIR/webapp"
chmod +x "$INSTALL_DIR/monitor" "$INSTALL_DIR/webapp"

# 更新前端资源
for f in webapp.html 404.html sw.js manifest.json offline.html config.example.json; do
  curl -sL "https://github.com/${REPO}/raw/main/${f}" -o "${INSTALL_DIR}/${f}" 2>/dev/null || true
done

# 记录版本
echo "$LATEST" > "$VERSION_FILE"

# 重启服务
systemctl start elec-monitor

echo "[✓] 已更新到 $LATEST"
rm -rf "$TMP"
UPDATEEOF

  chmod +x "${INSTALL_DIR}/update.sh"

  # 创建定时自动更新（每周一凌晨 3 点检查）
  cat > "/etc/systemd/system/${SERVICE_NAME}-update.service" <<EOF
[Unit]
Description=宿舍电费监控 - 自动更新
After=network.target

[Service]
Type=oneshot
ExecStart=${INSTALL_DIR}/update.sh
EOF

  cat > "/etc/systemd/system/${SERVICE_NAME}-update.timer" <<EOF
[Unit]
Description=宿舍电费监控 - 每周自动更新

[Timer]
OnCalendar=Mon *-*-* 03:00:00
RandomizedDelaySec=1h
Persistent=true

[Install]
WantedBy=timers.target
EOF

  systemctl daemon-reload
  systemctl enable "${SERVICE_NAME}-update.timer" 2>/dev/null || true
  systemctl start "${SERVICE_NAME}-update.timer" 2>/dev/null || true
  log "自动更新已启用（每周一凌晨 3 点 + 随机延迟）"
}

# ---- 创建管理命令别名 ----
setup_alias() {
  cat > "${INSTALL_DIR}/elec.sh" <<'ALIASEOF'
#!/bin/bash
# 宿舍电费监控 — 管理工具
# 用法: ./elec.sh <command>

CMD="${1:-help}"
SERVICE="elec-monitor"

case "$CMD" in
  start)
    systemctl start "$SERVICE"
    echo "[✓] 已启动"
    ;;
  stop)
    systemctl stop "$SERVICE"
    echo "[✓] 已停止"
    ;;
  restart)
    systemctl restart "$SERVICE"
    echo "[✓] 已重启"
    ;;
  status)
    systemctl status "$SERVICE" --no-pager -l
    ;;
  logs)
    journalctl -u "$SERVICE" -n 50 --no-pager -f
    ;;
  collect)
    /opt/elec-monitor/monitor --config-dir /opt/elec-monitor/data
    ;;
  update)
    /opt/elec-monitor/update.sh
    ;;
  token)
    echo "查看 token 状态:"
    /opt/elec-monitor/token_status --config-dir /opt/elec-monitor/data
    ;;
  config)
    echo "配置文件路径: /opt/elec-monitor/data/config.json"
    echo "Token 文件路径: /opt/elec-monitor/data/token.json"
    echo "SQLite 数据库: /opt/elec-monitor/data/electricity.db"
    if [ -f /opt/elec-monitor/data/config.json ]; then
      echo ""
      grep -E '"targets"|"username"|"poll_interval_minutes"|"rate_limit_per_minute"' /opt/elec-monitor/data/config.json
    fi
    ;;
  push-token)
    if [ -z "${2:-}" ]; then
      echo "用法: ./elec.sh push-token <服务器URL>"
      echo "需要 USTS_ADMIN_KEY 环境变量"
      exit 1
    fi
    ADMIN_KEY=$(cat /opt/elec-monitor/data/.admin_key 2>/dev/null || echo "")
    echo "服务器 ADMIN_KEY: $ADMIN_KEY"
    echo "推送 token 到 $2 ..."
    echo "本地执行: USTS_ADMIN_KEY=$ADMIN_KEY python3 login.py --push $2"
    ;;
  *)
    echo "宿舍电费监控 - 管理工具"
    echo ""
    echo "用法: ./elec.sh <command>"
    echo ""
    echo "命令:"
    echo "  start       启动服务"
    echo "  stop        停止服务"
    echo "  restart     重启服务"
    echo "  status      查看服务状态"
    echo "  logs        查看实时日志"
    echo "  collect     立即采集一次"
    echo "  update      检查并更新到最新版本"
    echo "  token       查看 token 状态"
    echo "  config      查看配置信息"
    echo "  push-token  推送 token 到远程服务器"
    echo ""
    echo "示例:"
    echo "  ./elec.sh status"
    echo "  USTS_ADMIN_KEY=xxx ./elec.sh push-token http://server:8080"
    ;;
esac
ALIASEOF

  chmod +x "${INSTALL_DIR}/elec.sh"
  ln -sf "${INSTALL_DIR}/elec.sh" /usr/local/bin/elec-monitor
  log "管理命令: elec-monitor (或 ${INSTALL_DIR}/elec.sh)"
}

# ---- 主流程 ----
echo ""
echo "=============================================="
echo "  宿舍电费监控 — 一键部署"
echo "=============================================="
echo ""

install_deps
download_release
setup_data_dir
setup_systemd
setup_timer
setup_autoupdate
setup_alias
start_service

echo ""
echo "=============================================="
echo "  部署完成!"
echo "=============================================="
echo ""
echo "  管理命令:"
echo "    elec-monitor status        查看状态"
echo "    elec-monitor logs          查看日志"
echo "    elec-monitor collect       立即采集"
echo "    elec-monitor update        检查更新"
echo "    elec-monitor token         查看 token"
echo "    elec-monitor config        查看配置"
echo ""
echo "  配置文件: ${DATA_DIR}/config.json"
echo "  仪表盘:   http://服务器IP:8080"
echo ""
echo "  登录（约 70 天一次）:"
echo "    本地运行: python3 login.py --push http://服务器IP:8080"
echo ""
echo "=============================================="