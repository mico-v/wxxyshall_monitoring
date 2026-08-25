# 配置文件说明

Linux 默认路径为 `/opt/elec/data/config.json`；Windows 默认路径为 `%LOCALAPPDATA%\WxxyshallMonitoring\data\config.json`。设置 `ELEc_DIR` 后，两端都改用 `$ELEc_DIR/data/config.json`。

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
  "rate_limit_per_minute": 30
}
```

字段说明：

- `username`：学号，不能为空；
- `port`：网页端口，范围 `1024..65535`；
- `base_url`：学校服务地址，通常无需修改；
- `poll_interval_minutes`：全局采集间隔，范围 `1..10080` 分钟；
- `rate_limit_per_minute`：学校接口请求速率，范围 `1..600`；
- `targets`：监控宿舍列表，数组顺序也是网页和手动批量采集顺序；
- `show_in_web`：可省略，默认 `true`。设为 `false` 后仍采集和入库，但不出现在公开网页、公开读数接口和 SSE；
- 宿舍内的 `poll_interval_minutes`：可省略。设置后覆盖该宿舍的全局采集间隔；
- `feeitemid`、`appId`：学校接口项目 ID，新添加宿舍通常使用默认值 `409` 和 `34`；
- `campus`、`building`、`room`：必须使用学校接口实际值，三者组合不能重复；
- `label`：网页显示名称。

配置和 token 支持热重载。宿舍、显示开关、采集间隔和限流修改后约两秒生效；端口变化需要重启。
