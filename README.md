# 宿舍电费监控

苏州科技大学宿舍电费余额采集与展示工具。单个 Go 二进制同时提供定时采集、SQLite 历史记录、网页仪表盘、SSE 实时读数和 PWA；`login.py` 负责在本地浏览器中获取 token。

服务端正式支持 Linux amd64/arm64；安装器面向 systemd。`login.py` 可在装有 Python、Playwright 和 Chrome/Edge 的桌面系统运行。

## 主要功能

- 定时或手动采集多个宿舍，结果写入 SQLite WAL 数据库。
- 定时/手动电费采集的每一次真实 HTTP 尝试（包括重试）都严格间隔 `1 分钟 / rate_limit_per_minute`。
- 批量采集可查看进度并真正取消正在等待或请求中的任务。
- 网页“查询设置”中的校区、楼栋和房间发现不占用电费采集的 `rate_limit_per_minute`；结果按查询参数缓存在进程内，后续访问直接读取内存，相同的并发首次查询只请求学校接口一次。
- 配置和 token 支持安全热重载；采集间隔、限流和目标顺序无需重启，已打开的仪表盘会在 30 秒内同步公开目标配置，端口变化需重启。
- 历史文件不自动清理；查询接口只返回符合条件的最新 10,000 条，按时间正序展示。
- 仪表盘按 `config.json` 中的目标顺序显示和采集。网页“查询设置”只提供级联添加宿舍；其他字段、排序和已有宿舍调整统一手工编辑 `config.json`。
- PWA 可安装，离线时使用最近缓存的公开配置和读数；管理请求和带鉴权请求绝不缓存。
- 所有修改、采集、发现、任务状态/取消和 token 推送接口都需要管理密钥。

项目没有自动更新功能。升级由管理员替换二进制并重启服务完成。

## 快速开始

### 获取 token

本机需要 Python 3、Playwright 和 Chrome/Edge：

```bash
pip install playwright requests
playwright install chromium

# 默认保存到当前目录 token.json；设置 ELEc_DIR 后保存到 ELEc_DIR/data/token.json
python3 login.py

# 只推送到服务器，不在本地保存
ADMIN_KEY='<管理密钥>' python3 login.py \
  --push https://elec.example.edu.cn --push-only

# 也可把密钥直接放在推送 URL 中
python3 login.py \
  --push 'https://elec.example.edu.cn/?key=<管理密钥>' --push-only

# 也可从文件读取密钥，避免出现在 shell 历史和进程环境中
python3 login.py --push https://elec.example.edu.cn --push-only \
  --admin-key-file /opt/elec/data/.admin_key
```

可用参数：

- `--output PATH`：指定本地 `token.json` 路径。
- `--browser PATH`：指定浏览器可执行文件。
- `--admin-key-file PATH`：从文件读取推送所需管理密钥。
- `--storage-state PATH`：显式保存包含敏感登录态的浏览器状态；默认不保存。
- `--push URL --push-only`：只向远程服务推送 token。

### 编译和本地运行

```bash
go build -o elec ./cmd/elec

# ELEc_DIR 指安装根目录，数据实际位于其 data/ 子目录
export ELEc_DIR="$PWD/.local-elec"
./elec run
```

### Windows 版本

更完整的图文式步骤见 [Windows 安装与使用](docs/WINDOWS.md)，所有配置字段见 [配置文件说明](docs/CONFIGURATION.md)。

Windows 使用单个 `elec.exe`，首次运行会自动复制到：

```text
%LOCALAPPDATA%\WxxyshallMonitoring\
├── elec.exe
└── data\
    ├── config.json
    ├── token.json
    ├── .admin_key
    ├── electricity.db
    └── elec.log
```

程序会为当前 Windows 用户写入 `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`，实现登录后自动启动。服务只监听 `127.0.0.1`，双击系统托盘图标或右键菜单中的“打开仪表盘 / 管理页面”即可访问同一个网页；不嵌入 WebView，也不需要管理员权限。

下载后直接双击 `elec.exe` 即可安装并启动。命令行方式：

```powershell
.\elec.exe install       # 安装并设置当前用户开机启动
.\elec.exe run           # 启动后台服务和托盘
.\elec.exe status        # 查看自启动状态
.\elec.exe config        # 查看配置目录
```

首次安装会自动打开带管理密钥的本机地址。密钥只保存在 `data\.admin_key`，不会写入注册表；网页读取 URL 中的 `key` 后会立即从地址栏移除。

如果需要自定义位置，可在启动前设置 `ELEc_DIR`：

```powershell
$env:ELEc_DIR = 'D:\WxxyshallMonitoring'
.\elec.exe run
```

### 手工交叉编译

```bash
# Linux
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o elec-linux-amd64 ./cmd/elec
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o elec-linux-arm64 ./cmd/elec

# Windows：隐藏控制台窗口，使用系统托盘
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
  go build -ldflags='-H=windowsgui' -o elec-windows-amd64.exe ./cmd/elec
```

GitHub Actions 位于 `.github/workflows/release.yml`。推送到 `main` 会构建并上传 Linux amd64/arm64、Windows amd64 工件；推送 `v*` 标签还会自动创建 GitHub Release 并附加压缩包。

首次运行会生成 `$ELEc_DIR/data/config.json` 和 `$ELEc_DIR/data/.admin_key`。打开 `http://localhost:8080`，进入“查询设置”时输入管理密钥，即可级联选择并添加宿舍。也可直接打开 `http://localhost:8080/?key=<管理密钥>`；网页读取后会把 `key` 从地址栏移除，并只保存到当前标签页的 `sessionStorage`。

### systemd 安装

```bash
sudo ./elec install
```

安装器会：

- 复制二进制到 `/opt/elec/elec`；
- 创建无登录权限的 `elec` 系统用户；
- 创建权限受限的 `/opt/elec/data`、`config.json` 和 `.admin_key`；
- 注册经过 `NoNewPrivileges`、`ProtectSystem`、能力清空等限制的 `elec.service`；
- 启用并重启服务。

定时采集由进程内循环按配置执行，不创建额外 systemd timer。

```bash
elec status
elec logs
elec collect
elec token
elec config       # 显示密钥文件位置，不直接打印密钥
```

## 配置

完整 `config.json`：

```json
{
  "username": "学号",
  "port": 8080,
  "base_url": "https://wxxyshall.usts.edu.cn",
  "targets": [
    {
      "feeitemid": 409,
      "appId": 34,
      "campus": "校区接口值",
      "building": "楼栋接口值",
      "room": "房间接口值",
      "label": "宿舍显示名称",
      "show_in_web": true,
      "poll_interval_minutes": 30
    }
  ],
  "poll_interval_minutes": 60,
  "rate_limit_per_minute": 30
}
```

约束：

- `username` 非空；`port` 为 `1024..65535`，与默认无特权 systemd 服务保持一致。
- `base_url` 必须是有效的 HTTP/HTTPS 地址。
- `poll_interval_minutes` 为 `1..10080`。
- `rate_limit_per_minute` 为 `1..600`。例如 `30` 表示任意两次学校 HTTP 请求至少间隔 2 秒，并非一分钟突发 30 次。
- 每个目标的 `feeitemid`、`appId` 必须为正整数，`campus/building/room` 非空且组合不可重复。
- 目标的 `show_in_web` 可省略，默认 `true`；设为 `false` 后仍会定时/手动采集，但不会出现在公开配置、读数、宿舍页面或 SSE 中。
- 目标的 `poll_interval_minutes` 可省略；省略时继承全局 `poll_interval_minutes`，设置后以该宿舍的 `1..10080` 分钟周期覆盖全局值。
- `targets` 数组顺序就是仪表盘和批量采集顺序。

网页“查询设置”仅显示已有宿舍，并允许通过校区→楼栋→房间级联添加宿舍；添加后自动保存。网页不编辑服务字段、目标字段和顺序，也不提供删除操作。需要修改、排序或删除时由管理员直接编辑 `config.json`，保存后会热重载。systemd 安装环境中请保持文件归属 `elec:elec`、权限 `0640`，数据目录不允许符号链接或特殊文件。

## 安全边界

- 管理密钥存放于数据目录的 `.admin_key`，服务启动时读取；不会写入 `config.json` 或 systemd unit。
- 浏览器只把密钥放在 `sessionStorage`，关闭标签页后失效。公开仪表盘、读数和读数 SSE 不需要密钥。
- 管理 API 使用 `Authorization: Bearer <key>`，也接受 `?key=<key>`。生产部署建议放在 HTTPS 反向代理后；查询参数可能出现在代理访问日志中，应限制日志访问并避免分享含密钥的原始链接。
- JSON 请求限制为 1 MiB、拒绝未知字段和多余 JSON；配置、token 使用临时文件 + `fsync` + 原子替换保存。
- 默认安装服务不以 root 运行，数据目录及敏感文件采用受限权限。
- 历史记录没有删除 API 或自动清理任务；请自行备份整个数据目录。

## API

公开只读接口：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/`, `/room/{campus}/{building}/{room}` | 仪表盘 |
| GET | `/api/health` | 服务和数据库健康状态 |
| GET | `/api/readings?days&campus&building&room` | 最新最多 10,000 条读数 |
| GET | `/api/config` | 目标顺序、默认 fee item/app ID；不暴露服务敏感配置 |
| GET | `/api/events` | 新读数 SSE |

需要 Bearer 管理密钥：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET/POST | `/api/config` | 鉴权 GET 返回完整配置；POST 更新完整或部分字段 |
| POST | `/api/collect` | 采集一间已配置宿舍 |
| POST | `/api/collect-all` | 启动批量采集 |
| GET | `/api/collect-all/status?job_id=...` | 查询任务快照 |
| POST | `/api/collect-all/cancel?job_id=...` | 取消 ID 精确匹配的当前批量任务 |
| GET | `/api/campuses`, `/api/buildings`, `/api/rooms` | 级联发现，支持 `feeitemid` 和 `appId` |
| POST | `/api/token` | 推送 token |
| POST | `/api/admin/verify` | 校验管理密钥 |

接口字段约定：

- `POST /api/config` 接受配置章节列出的六个顶层字段并支持部分更新；网页添加宿舍时也可单独提交 `{"target": {...}}`。新宿舍追加到列表末尾；重复宿舍只原位更新 `label`，保留其 `feeitemid`、`appId` 和顺序，不覆盖其他配置。`target` 不能与其他配置字段混用；未知字段或无效范围会返回 `400`。
- `POST /api/collect` 请求体为 `{"campus":"...","building":"...","room":"..."}`；三个字段必须同时提供且目标必须已配置。空对象表示采集配置第一项。
- 发现接口使用查询参数 `feeitemid`、`appId`；楼栋接口另需 `campus`，房间接口另需 `campus`、`building`。这些请求不受 `rate_limit_per_minute` 限制；成功结果按 `base_url + feeitemid + appId + 层级参数` 缓存到当前进程，直到服务重启；相同并发请求会合并，错误不缓存，`base_url` 变化会使用新的缓存键。
- 批量任务状态为 `queued`、`running`、`cancelling`、`cancelled`、`done` 或 `failed`。运行中状态只返回进度计数，终态返回完整 `results`；取消必须传启动接口返回的同一个 `job_id`。
- `POST /api/token` 接受 `access_token`、可选 `refresh_token`、`expires_in`、`login_time`、`sno` 和 `source`，拒绝未知字段及过大的 token。

## 数据与 PWA

- SQLite 文件：`$ELEc_DIR/data/electricity.db`，包含全部历史；接口限制不删除数据库记录。
- Service Worker 只缓存同源公开 GET 请求。写请求、带 `Authorization` 的请求和 SSE 永不缓存。
- 导航采用网络优先并回退到应用壳；公开 API 采用网络优先并回退到完全匹配（包括查询参数）的缓存。
- PWA manifest 使用 192/512 PNG maskable 图标。

## 开发验证

```bash
GOCACHE=/tmp/wxxyshall-go-cache go test ./...
GOCACHE=/tmp/wxxyshall-go-cache go vet ./...
GOCACHE=/tmp/wxxyshall-go-cache go build ./...
GOCACHE=/tmp/wxxyshall-go-cache go test -race ./...

node --check internal/web/static/sw.js
python3 -m py_compile login.py
```

项目结构：

```text
cmd/elec/          CLI、安装器、HTTP 服务和定时循环
internal/collector 统一定时/单间/批量采集与并发边界
internal/charge/   学校接口客户端及错误处理
internal/config/   配置/token 原子读写和热重载
internal/db/       SQLite 历史记录
internal/rate/     严格请求间隔器
internal/web/      API、SSE、嵌入式前端和 PWA
login.py           本地浏览器登录与 token 推送
```
