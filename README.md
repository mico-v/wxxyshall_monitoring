# 宿舍电费监控

苏州科技大学校园平台宿舍电费监控系统。自动登录自己的账号，定时查询宿舍电费余额，记录到 SQLite，网页展示曲线。支持多宿舍监控，SSE 实时推送，可安装为 PWA 应用。

## 快速开始

### 1. 获取 token（约 70 天一次）

```bash
# 需要本地有 Python 3 + Playwright，且能弹出浏览器
pip install playwright
playwright install chromium

# 仅保存本地
python3 login.py

# 保存本地并推送到远程服务器
ADMIN_KEY=<key> python3 login.py --push http://服务器IP:8080
```

浏览器会自动弹出来，学号已预填好，你手动点密码（安全键盘）→ 点登录即可。脚本会自动捕获 token 保存到 `token.json`，如果指定了 `--push` 还会推送到远程服务器。

### 2. 本地运行

```bash
# 编译
go mod tidy
go build -o monitor ./cmd/monitor/
go build -o webapp ./cmd/webapp/

# 单次采集测试
./monitor

# 启动仪表盘
./webapp
# 打开 http://localhost:8080

# 常驻采集（按 config poll_interval_minutes 循环）
./monitor --loop &

# 仪表盘前台运行
./webapp --host 0.0.0.0 --port 8080
```

### 3. 服务器部署

```bash
# 一键部署（需要 systemd, curl, jq）
curl -sL https://github.com/mico-v/wxxyshall_monitoring/releases/latest/download/deploy.sh | sudo bash
```

部署后：

```bash
elec-monitor status          # 服务状态
elec-monitor logs            # 实时日志
elec-monitor collect         # 立即采集一次
elec-monitor update          # 检查更新
elec-monitor token           # 查看 token 状态
elec-monitor config          # 查看配置信息

# 配置文件路径
/opt/elec-monitor/data/config.json

# 查看 ADMIN_KEY（推送 token 时用）
cat /opt/elec-monitor/data/.admin_key

# 推送 token 到服务器（约 70 天一次，在本地机器执行）
ADMIN_KEY=<key> python3 login.py --push http://服务器IP:8080
```

### 4. 配置

编辑 `config.json`（数据目录下）：

```json
{
  "username": "学号",
  "targets": [],
  "poll_interval_minutes": 60,
  "rate_limit_per_minute": 30,
  "base_url": "https://wxxyshall.usts.edu.cn"
}
```

也可以通过网页「查询设置」→ 级联选择校区/楼栋/房间来添加监控宿舍。

> **注意**：`rate_limit_per_minute` 是硬设置，只能手改 config.json，网页改不掉。

## 项目结构

```
├── cmd/
│   ├── monitor/main.go          # 采集守护进程
│   ├── webapp/main.go           # HTTP 仪表盘
│   └── tools/                   # CLI 工具
├── internal/
│   ├── config/                  # 配置/token 读写
│   ├── charge/                  # 学校 API 客户端
│   ├── auth/                    # Token 过期检查
│   ├── db/                      # SQLite 操作
│   ├── web/                     # HTTP 路由 + SSE 推送
│   └── rate/                    # 滑窗限流器
├── webapp.html                  # SPA 前端
├── sw.js                        # Service Worker
├── manifest.json                # PWA 清单
├── offline.html                 # 离线回退页
├── static/echarts.min.js        # 图表库
├── login.py                     # 浏览器登录（Playwright）
├── deploy.sh                    # 一键部署脚本
└── .github/workflows/release.yml  # CI/CD
```

## 架构

```
用户浏览器 ──→ webapp (:8080) ──→ SQLite (electricity.db)
                 │
                 ├── SSE 实时推送 (GET /api/events)
                 ├── 学校 API (wxxyshall.usts.edu.cn)
                 └── 静态文件 (webapp.html, echarts)

monitor (后台进程) ──→ 学校 API ──→ SQLite
```

- **login.py**（本地运行，~70 天一次）：弹浏览器登录 → 保存 token → `push_token` 推到服务器
- **monitor**：定时采集，按 `(feeitemid, appId)` 复用会话，查余额写入 SQLite
- **webapp**：HTTP 仪表盘，Go 标准库 `net/http`，SSE 实时推送，限流保护学校 API

## API 路由

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/` | 仪表盘 SPA |
| GET | `/room/{campus}/{building}/{room}` | 单宿舍独立界面 |
| GET | `/api/events` | SSE 实时推送 |
| GET | `/api/readings?days&campus&building&room` | 读数 JSON |
| GET/POST | `/api/config` | 读/改配置 |
| POST | `/api/collect` | 立即采集单间 |
| POST | `/api/collect-all` | 批量采集全部 |
| GET | `/api/collect-all/status?job_id=` | 采集进度 |
| GET | `/api/campuses|buildings|rooms` | 发现接口 |
| POST | `/api/token` | 推送 token（ADMIN_KEY 保护） |
| GET | `/api/health` | 健康检查 |

## 前端特性

- **SSE 实时推送**：采集完成后页面自动更新，无需手动刷新
- **PWA 可安装**：支持添加到桌面，类原生应用体验
- **Service Worker 缓存**：离线时显示上次缓存数据
- **每间宿舍独立 URL**：`/room/<校区>/<楼栋>/<房间>`，可收藏/分享
- **浅/深色主题**：跟随系统或手动切换
- **ECharts 图表**：平滑面积曲线，悬停 tooltip，y 轴自适应小数位

## 关键约定

- **token 约 70 天过期**，服务端续期不可用，到期需重新 `login.py` + `push_token`
- **`rate_limit_per_minute`** 保护学校 WAF，只能手改 config.json
- **数据库**：SQLite，WAL 模式，与 Python 版共用 `electricity.db`

## 开发

```bash
go mod tidy
go build ./...
go vet ./...
```

## 数据目录

默认在项目根目录。设环境变量 `USTS_DATA_DIR` 可改变路径（config.json / token.json / electricity.db 一起搬过去）。

```bash
USTS_DATA_DIR=/path/to/data ./webapp
```