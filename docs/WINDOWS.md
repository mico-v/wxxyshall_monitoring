# Windows 安装与使用

## 安装

从 GitHub Actions 或 Release 下载 `elec-windows-amd64.zip`，解压后双击其中的 `elec.exe`。

程序会自动完成以下操作：

1. 复制自身到 `%LOCALAPPDATA%\WxxyshallMonitoring\elec.exe`；
2. 创建 `%LOCALAPPDATA%\WxxyshallMonitoring\data`；
3. 生成 `config.json` 和 `.admin_key`；
4. 写入当前用户的注册表开机启动项；
5. 启动本机 HTTP 服务和系统托盘；
6. 打开 `http://127.0.0.1:5009/?key=...`。

不需要管理员权限。Windows 版本默认只监听 `127.0.0.1`，局域网内其他设备不能直接访问。

## 系统托盘

- 双击：打开仪表盘和管理页面；
- 右键 → 打开仪表盘 / 管理页面；
- 右键 → 打开配置目录；
- 右键 → 打开运行日志；
- 右键 → 开机自动启动：勾选或取消当前用户自启动；
- 右键 → 退出：停止采集和本地网页服务。

再次双击任意位置的 `elec.exe` 不会启动第二个服务，而是打开已经运行的仪表盘。

## 登录学校账号

打开仪表盘，进入「查询设置 → 学校账号」，输入学号和密码即可获取 token。该流程只使用 `elec.exe` 和学校网络，不需要安装 Python、Playwright 或额外的 Chromium。服务端会按学校本次安全键盘的可见键位替换表转换密码；密码只在本次请求中使用，不会保存到磁盘或日志。

登录成功后页面会显示 token 剩余有效期。token 通常约 70 天有效，过期后在同一位置重新登录即可。

## 命令行备用方式

无法使用网页登录时，压缩包中的 `login.py` 仍可通过本地浏览器获取并推送 token。只有这条备用路径需要 Python 3、Playwright、requests 和 Chrome/Edge：

```powershell
pip install playwright requests
playwright install chromium

$root = Join-Path $env:LOCALAPPDATA 'WxxyshallMonitoring'
$key = (Get-Content (Join-Path $root 'data\.admin_key') -Raw).Trim()
python .\login.py --push "http://127.0.0.1:5009/?key=$key" --push-only
```

登录成功后，token 会直接推送给后台程序。通常只需在 token 到期前重新执行；网页登录与命令行推送二选一即可。

## 修改配置

右键托盘图标选择“打开配置目录”，编辑 `data\config.json`。保存后约两秒自动生效；端口修改需要退出并重新启动程序。

完整字段说明见 [CONFIGURATION.md](CONFIGURATION.md)。

## 更新与移除

更新：先从托盘退出旧版本，再双击新版本 EXE，新文件会覆盖安装目录中的 `elec.exe`，数据目录不会删除。

移除：先在托盘菜单取消“开机自动启动”，再退出程序，最后删除：

```text
%LOCALAPPDATA%\WxxyshallMonitoring
```

删除整个目录会同时删除历史数据库、token、配置和管理密钥，请先备份 `data`。
