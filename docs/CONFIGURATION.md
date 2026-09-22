# 配置文件说明

Linux 默认路径为 `/opt/elec/data/config.json`；Windows 默认路径为 `%LOCALAPPDATA%\WxxyshallMonitoring\data\config.json`。设置 `ELEc_DIR` 后，两端都改用 `$ELEc_DIR/data/config.json`。

```json
{
  "username": "学号",
  "port": 5009,
  "base_url": "https://elec.example.edu.cn",
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
  "admin_auth_enabled": false,
  "show_homepage": true,
  "allow_guest_add_target": true,
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

- `username`：学号，不能为空；
- `port`：网页端口，范围 `1024..65535`；
- `base_url`：学校服务地址，通常无需修改；
- `poll_interval_minutes`：全局采集间隔，范围 `1..10080` 分钟；
- `rate_limit_per_minute`：定时和手动电费采集的学校接口请求速率，范围 `1..600`；网页添加宿舍时使用的校区、楼栋、房间发现接口不受此项限制，由进程内缓存和并发合并管理；
- `admin_auth_enabled`：管理接口是否要求管理密钥，默认 `false`。关闭时仍会生成和保留 `.admin_key`，且密钥校验接口 `/api/admin/verify` 始终严格验证；
- `show_homepage`：是否公开显示全部宿舍主页，默认 `true`。设为 `false` 后，未登录访客打开主页看到的是一个与单宿舍页「查询设置」一致的宿舍选择器（可跳转或添加宿舍），看不到聚合数据；已登录（浏览器保存了有效管理密钥）时主页照常直接显示聚合仪表盘，无需重复输入密钥。单宿舍页不显示返回主页按钮。无论该开关如何取值，单宿舍页始终公开可访问；
- `allow_guest_add_target`：未登录访客能否添加宿舍，默认 `true`（与旧版本行为一致）。仅在 `admin_auth_enabled=false` 时生效——管理鉴权开启时任何添加都需要密钥。设为 `false` 后，网页「添加宿舍」和 `POST /api/config` 的单宿舍添加都需要带管理密钥，否则返回 401；
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

配置和 token 支持热重载。webhook、宿舍、主页/鉴权开关、访客添加开关、显示开关、采集间隔和限流修改后约两秒生效；端口变化需要重启。webhook 消息发送成功和失败都会记录日志，但不会使本次采集失败。
