# 配置文件说明

Linux 默认路径为 `/opt/elec/data/config.json`；Windows 默认路径为 `%LOCALAPPDATA%\WxxyshallMonitoring\data\config.json`。设置 `ELEC_DIR` 后，两端都改用 `$ELEC_DIR/data/config.json`。

```json
{
  "username": "学号",
  "allow_public_add_target": true,
  "port": 5009,
  "bind": "127.0.0.1",
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
      "poll_interval_minutes": 30,
      "notify_mode": "alert",
      "notify_time": "08:00",
      "webhook": {
        "enabled": true,
        "url": "https://example.com/room-webhook",
        "token": "该宿舍专用 TOKEN",
        "body": {
          "content": "{{label}} 当前余额：{{surplus_charge}}",
          "room_id": "{{room}}"
        }
      }
    },
    {
      "feeitemid": 409,
      "appId": 34,
      "campus": "另一校区",
      "building": "另一楼栋",
      "room": "另一房间",
      "label": "只采集、不公开显示",
      "show_in_web": false
    }
  ],
  "poll_interval_minutes": 60,
  "rate_limit_per_minute": 30,
  "webhook": {
    "enabled": false,
    "url": "http://127.0.0.1:9966/send",
    "token": "",
    "notify_mode": "low_balance",
    "low_balance_threshold": 10,
    "body": {
      "content": "【电费监控】{{label}} 当前余额：{{surplus_charge}}，采集时间：{{ts}}",
      "umo": "mybot:FriendMessage:1000000"
    }
  }
}
```

字段说明：

- `username`：学号，不能为空。通过网页成功登录学校账号后会自动更新为本次登录的学号；密码不写入配置；
- `allow_public_add_target`：是否开放公开添加宿舍，默认 `true`。开启时未登录主页显示宿舍选择器，可公开添加并跳转；关闭后未登录主页只显示登录入口。登录后主页固定显示已有宿舍列表，添加宿舍统一从「查询设置」操作。公开添加固定限制为每 IP 每分钟 6 次、突发 3 次，宿舍总数最多 200 间；
- `port`：网页端口，范围 `1024..65535`；
- `bind`：网页监听地址，可填 IP（含 IPv6）或主机名，端口仍由 `port` 指定。**留空即沿用平台默认**：Linux 监听所有地址（`0.0.0.0`），Windows/其它平台仅监听 `127.0.0.1`。用 Caddy/Nginx 反代时建议设为 `"127.0.0.1"`，否则明文端口会绕过反代直接暴露；写成 `"127.0.0.1:5009"` 这类带端口的格式会被拒绝（端口请用 `port`）；
- `base_url`：学校服务地址，通常无需修改；
- `poll_interval_minutes`：全局采集间隔，范围 `1..10080` 分钟；
- `rate_limit_per_minute`：定时和手动电费采集的学校接口请求速率，范围 `1..600`；网页添加宿舍时使用的校区、楼栋、房间发现接口不受此项限制，由进程内缓存和并发合并管理；
- `webhook`：采集通知配置。`enabled` 为 `true` 时向 `url` 发送 `webhook.body` 中定义的 JSON 对象，并使用 `Authorization: Bearer <token>` 鉴权。`body` 支持任意 JSON 字段、嵌套对象和数组；程序不会内置或追加 `content`、`umo` 等字段。每个成功采集的宿舍会按照自己的 `notify_mode` 决定是否发送。
- `webhook.body` 中的字符串支持 `{{label}}`、`{{campus}}`、`{{building}}`、`{{room}}`、`{{ts}}`、`{{surplus_charge}}`、`{{low_balance_threshold}}`、`{{total_usage}}` 占位符，替换会递归应用到嵌套对象和数组。旧版顶层 `umo` / `content_template` 配置会自动迁移到 `body`。
- `webhook.notify_mode` 及 `low_balance_threshold` 为全局 webhook 的兼容配置；宿舍未设置 `notify_mode` 时，仍可使用 `low_balance`、`balance_decrease` 或 `every_collection` 规则。新配置建议使用宿舍级 `notify_mode`。
- `targets`：监控宿舍列表，数组顺序也是网页和手动批量采集顺序；
- `show_in_web`：可省略，默认 `true`。设为 `false` 后仍采集和入库，但不出现在公开网页、公开读数接口和 SSE；
- 宿舍内的 `poll_interval_minutes`：可省略。设置后覆盖该宿舍的全局采集间隔；
- `feeitemid`、`appId`：学校接口项目 ID，新添加宿舍通常使用默认值 `409` 和 `34`；
- `campus`、`building`、`room`：必须使用学校接口实际值，三者组合不能重复；
- `label`：网页显示名称。
- 目标的 `notify_mode`：`none` 表示不通知，`daily` 表示每天在 `notify_time` 到达后的首次成功采集时通知一次，`alert` 表示仅在余额首次降至低值阈值时通知；省略时保持旧行为，按全局 webhook 的 `notify_mode` 判断。
- 目标的 `notify_time`：每日通知时间，使用 `HH:MM` 格式，仅 `daily` 模式使用；省略时默认为 `08:00`。每日通知按宿舍分别去重，发送失败会在下次成功采集时重试。
- 目标的 `webhook`：可选的完整 webhook 覆盖配置。省略时使用全局 `webhook`；配置后覆盖该宿舍的 `enabled`、`url`、`token`、阈值和 `body` 等设置。设置 `webhook.enabled=false` 可单独关闭该宿舍通知。

配置和 token 支持热重载。公开添加开关、webhook、宿舍、显示开关、采集间隔和限流修改后约两秒生效；端口变化需要重启。管理接口和聚合数据固定需要管理密钥，不提供关闭鉴权的配置。webhook 消息发送成功和失败都会记录日志，但不会使本次采集失败。

## 学校登录与 Token

推荐在网页「查询设置 → 学校账号」中登录。服务端会请求学校本次安全键盘映射，按可见的 QWERTY、数字和符号键位把密码转换成一次性提交字符，再把返回的 token 原子写入 `token.json`。账号密码不会写入配置或日志。

网页会显示 token 剩余天数。token 通常约 70 天有效，续期接口不可用；到期后在同一表单重新登录即可。`login.py` 只作为命令行备用方案。
