#!/bin/bash
# 宿舍电费监控 — 一键部署脚本
# 用法: curl -sL https://github.com/mico-v/wxxyshall_monitoring/releases/latest/download/deploy.sh | bash
set -euo pipefail

REPO="mico-v/wxxyshall_monitoring"
GITHUB="https://github.com/${REPO}"
API="https://api.github.com/repos/${REPO}/releases/latest"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
log()  { echo -e "${GREEN}[✓]${NC} $1"; }
warn() { echo -e "${YELLOW}[!]${NC} $1"; }
err()  { echo -e "${RED}[✗]${NC} $1"; exit 1; }

if [ "$(id -u)" -ne 0 ]; then
  err "请以 root 运行: sudo bash deploy.sh"
fi

ARCH=$(uname -m)
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  *)       err "不支持的架构: $ARCH" ;;
esac
log "系统架构: linux-${ARCH}"

# 安装依赖
command -v curl >/dev/null || apt-get install -y -qq curl
command -v jq   >/dev/null || apt-get install -y -qq jq

# 获取最新版本
LATEST=$(curl -sL "$API" | jq -r '.tag_name // empty')
if [ -z "$LATEST" ]; then
  warn "无法获取最新版本，将使用 latest 标签"
  LATEST="latest"
fi
log "最新版本: ${LATEST}"

# 下载
URL="${GITHUB}/releases/download/${LATEST}/elec-linux-${ARCH}.gz"
log "下载: ${URL}"
curl -sL "$URL" | gunzip > /tmp/elec
chmod +x /tmp/elec

# 安装
/tmp/elec install

rm -f /tmp/elec
echo ""
echo "部署完成! 管理命令:"
echo "  elec status      查看状态"
echo "  elec logs        查看日志"
echo "  elec collect     立即采集"
echo "  elec update      检查更新"
echo "  elec token       查看 token"
echo "  elec config      查看配置"
echo ""
echo "登录（约 70 天一次）:"
echo "  ADMIN_KEY=<key> python3 login.py --push http://服务器IP:8080"
echo ""
echo "（ADMIN_KEY 查看: elec config）"