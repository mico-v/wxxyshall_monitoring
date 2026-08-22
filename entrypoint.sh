#!/bin/sh
# 启动:常驻采集(后台)+ 仪表盘(前台)。
# 首次运行若无 config.json,从示例复制一份(用户名等需在网页"查询设置"里填/改)。
set -e
cd /app

mkdir -p "$USTS_DATA_DIR"
if [ ! -f "$USTS_DATA_DIR/config.json" ]; then
  cp /app/config.example.json "$USTS_DATA_DIR/config.json"
  echo "[init] 已创建默认 config.json —— 用网页\"查询设置\"填写学号与宿舍"
fi

# ADMIN_KEY 未配置(或仍是占位 change-me)时自动生成,并打印到日志方便查询。
# 生成结果持久化到数据目录 .admin_key,重启后保持不变;用户显式设置时尊重用户取值。
key_file="$USTS_DATA_DIR/.admin_key"
if [ -z "${ADMIN_KEY}" ] || [ "${ADMIN_KEY}" = "change-me" ]; then
  if [ -s "$key_file" ]; then
    ADMIN_KEY="$(cat "$key_file")"
  else
    ADMIN_KEY="$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')"
    printf '%s\n' "$ADMIN_KEY" > "$key_file"
    chmod 600 "$key_file"
  fi
  export ADMIN_KEY
  echo "[init] ADMIN_KEY 未配置,已自动生成(持久化于 $key_file):"
  echo "[init]   >>>>>>>>>> ADMIN_KEY=$ADMIN_KEY >>>>>>>>>>"
  echo "[init]   推送 token 时用: USTS_ADMIN_KEY=$ADMIN_KEY push_token http://<服务器>:8080"
fi

echo "[*] 启动 monitor 循环(采集)..."
monitor --loop --config-dir "$USTS_DATA_DIR" &
echo "[*] 启动 webapp 仪表盘... http://0.0.0.0:${PORT:-8080}"
exec webapp --host 0.0.0.0 --port "${PORT:-8080}" --config-dir "$USTS_DATA_DIR"