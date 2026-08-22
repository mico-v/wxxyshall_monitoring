# 宿舍电费监控

苏州科技大学校园平台宿舍电费监控系统。

自动登录账号，定时查询宿舍电费余额，记录到 SQLite，网页展示曲线。支持多宿舍监控、SSE 实时推送、PWA 可安装。

## 功能

| 功能 | 说明 |
|---|---|
| 自动采集 | 定时循环采集电费余额，写入 SQLite（WAL 模式） |
| 网页仪表盘 | 纯 SPA 前端，显示剩余电量曲线、读数明细、KPI 卡片 |
| SSE 实时推送 | 采集完成后页面自动更新，无需手动刷新 |
| 批量采集 | 一次采集所有监控宿舍，实时进度推送，可取消 |
| 多宿舍管理 | 每间宿舍独立 URL `/room/校区/楼栋/房间`，可收藏/分享 |
| 级联发现 | 网页端级联选择校区→楼栋→房间，自动添加监控目标 |
| 轻量图表 | 内建 Canvas 渲染，平滑曲线 + 渐变填充 + hover tooltip，无外部依赖 |
| 限流保护 | 滑动窗口限流器，保护学校 WAF |
| 浅/深色主题 | 跟随系统或手动切换，持久化到 localStorage |
| PWA 可安装 | 支持添加到桌面，Service Worker 离线回退 |
| 结构化日志 | log/slog，可配合 journald 直接查看 |
| 自安装 | `elec install` 自动注册 systemd 服务 + 定时器 |
| 自动更新 | `elec update` 检查 GitHub Release 并更新二进制 |
| CLI 工具集 | `elec status`、`elec logs`、`elec token`、`elec config`、`elec collect` |

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

浏览器会自动弹出，学号已预填，手动点击密码（安全键盘）→ 点登录即可。脚本自动捕获 token 保存到本地 `token.json`，如果指定了 `--push` 还会推送到远程服务器。

### 2. 编译

```bash
go mod tidy
go build -o elec ./cmd/elec/
```

### 3. 本地运行

```bash
# 单次采集测试
./elec collect

# 启动服务（webapp + 定时采集循环）
./elec run
# 打开 http://localhost:8080
```

### 4. 服务器部署（自安装）

```bash
# 将编译好的二进制传到服务器，然后：
sudo ./elec install
```

`elec install` 会做以下事情：

- 复制自身到 `/opt/elec/elec`
- 生成默认配置 `/opt/elec/data/config.json`
- 生成随机管理密钥 `/opt/elec/data/.admin_key`
- 注册 systemd 服务 `elec.service`（开机自启 + 崩溃自动重启）
- 创建定时采集 timer `elec-collect.timer`（每 60 分钟自动采集一次）

完成后：

```bash
systemctl start elec         # 启动服务
elec status                  # 查看状态
elec logs                    # 查看实时日志
elec collect                 # 立即采集一次
elec update                  # 检查更新
elec token                   # 查看 token 状态
elec config                  # 查看配置信息

# 查看 ADMIN_KEY（推送 token 时用）
cat /opt/elec/data/.admin_key

# 推送 token 到服务器（约 70 天一次，在本地机器执行）
ADMIN_KEY=<key> python3 login.py --push http://服务器IP:8080
```

### 5. 配置

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

> **注意**：`rate_limit_per_minute` 只能手改 config.json，网页改不掉。

## CLI 命令参考

| 命令 | 说明 |
|---|---|
| `elec` / `elec run` | 启动服务（webapp + 采集循环） |
| `elec install` | 自安装到 `/opt/elec/` + 注册 systemd |
| `elec collect` | 单次采集所有监控宿舍 |
| `elec status` | 查看 systemd 服务状态 |
| `elec logs` | 查看实时日志（journalctl -f） |
| `elec update` | 检查 GitHub Release 并更新 |
| `elec token` | 查看 token 状态和有效期 |
| `elec config` | 查看安装路径、配置信息 |
| `elec help` | 显示帮助 |

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
| POST | `/api/collect-all/cancel` | 取消批量采集 |
| GET | `/api/campuses|buildings|rooms` | 发现接口 |
| POST | `/api/token` | 推送 token（ADMIN_KEY 保护） |
| GET | `/api/health` | 健康检查 |

## 项目结构

```
├── cmd/
│   ├── elec/main.go              # 主入口：CLI + HTTP 服务器 + 采集循环
│   └── tools/                    # CLI 辅助工具
│       ├── discover/             # 列出校区/楼栋/房间
│       ├── query/                # 直接查询任意房间电费
│       ├── report/               # 查看电费历史记录
│       └── token_status/         # 查看 token 状态
├── internal/
│   ├── config/                   # 配置/token 读写 + 嵌入默认配置
│   ├── charge/                   # 学校电费 API 反向封装
│   ├── auth/                     # Token 过期检查
│   ├── db/                       # SQLite 操作（WAL 模式）
│   ├── web/                      # HTTP 路由 + SSE 推送 + 嵌入静态文件
│   │   └── static/               # 前端文件（唯一源，嵌入到二进制）
│   └── rate/                     # 滑动窗口限流器
├── login.py                      # 浏览器登录（Playwright）
└── .github/workflows/release.yml # CI/CD
```

所有前端文件（webapp.html、sw.js、manifest.json、404.html、offline.html、PWA 图标）通过 `//go:embed` 嵌入到二进制中，部署只需一个二进制文件，无需额外复制静态文件。

## 架构

```
用户浏览器 ──→ elec (:8080) ──→ SQLite (electricity.db)
                 │
                 ├── SSE 实时推送 (GET /api/events)
                 ├── 学校 API (wxxyshall.usts.edu.cn)
                 └── 静态文件 (嵌入到二进制)
```

- **login.py**（本地运行，~70 天一次）：弹浏览器 → 登录 → 捕获 token → 保存本地 → 可选推送到服务器
- **elec**（单二进制）：定时采集 + HTTP 仪表盘，按 `(feeitemid, appId)` 复用会话，查余额写入 SQLite，Go 标准库 `net/http`，SSE 实时推送，限流保护学校 API

## 前端特性

- **SSE 实时推送**：采集完成后页面自动更新，无需手动刷新
- **PWA 可安装**：支持添加到桌面，类原生应用体验
- **Service Worker 缓存**：离线时显示上次缓存数据
- **每间宿舍独立 URL**：`/room/<校区>/<楼栋>/<房间>`，可收藏/分享
- **浅/深色主题**：跟随系统或手动切换，持久化到 localStorage
- **轻量 Canvas 图表**：平滑曲线、渐变填充、hover tooltip、端标签、自适应小数位
- **多宿舍视图**：KPI 卡片 + mini 趋势图，点击放大查看单间详情
- **采集进度**：批量采集时实时显示进度百分比，可取消
- **响应式设计**：桌面、平板、手机端自适应布局

## 关键约定

- **token 约 70 天过期**，服务端续期不可用，到期需重新 `login.py` + `push_token`
- **`rate_limit_per_minute`** 保护学校 WAF，只能手改 config.json
- **数据库**：SQLite，WAL 模式，支持并发读写

## 开发

```bash
go mod tidy
go build ./...
go vet ./...
go test ./...
```

## 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `ELEc_DIR` | `/opt/elec` | 数据目录（config.json、token.json、electricity.db 都在其下） |
| `ADMIN_KEY` | 自动生成 | 管理密钥，用于 token 推送鉴权 |

```bash
ELEc_DIR=/path/to/data ./elec run
```