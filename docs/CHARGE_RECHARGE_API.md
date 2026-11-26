# 宿舍购电充值接口调研

本文记录学校“宿舍购电”页面中充值记录、订单和校园卡支付接口的逆向结果，以及当前 `/user` 个人入口的实现边界。

## 页面入口

- H5 宿舍购电：`GET /charge/feeitem/toAppitem?feeitemid=409&appId=34&loginFrom=h5`
- 该入口会跳转到 `/charge-app/`，页面标题为“宿舍购电”
- PC 菜单入口：`/charge-pc/pays/409`
- 电费项目 ID：`409`

请求 charge 子系统时需要同时携带：

```http
Authorization: Basic Y2hhcmdlOmNoYXJnZV9zZWNyZXQ=
synjones-auth: bearer <access_token>
synAccessSource: h5
```

服务端还会使用 charge 会话 Cookie。仅持有 access token 时，建议先经过现有 `internal/charge` 的会话建立流程。

## 与当前“充电记录”的区别

当前项目的 `/api/recharges` 来自电表历史数据：

```text
总用电量发生跳升 => 推导为一次充值事件
```

它适合画余额/用量趋势，但不是学校支付订单流水，无法获得：

- 实际支付时间
- 支付方式
- 订单号
- 支付状态
- 退款状态
- 校园卡账户余额

学校真实数据应使用 `/charge/feeitem/getRechargeRecord` 和 `/charge/order/*`。

## 房间定位

充值记录必须关联具体房间。三级别名选择仍然使用：

```http
POST /charge/feeitem/getThirdData
Content-Type: application/x-www-form-urlencoded
```

逐级表单：

```text
feeitemid=409&type=select&level=0
feeitemid=409&type=select&level=1&campus=<campus>
feeitemid=409&type=select&level=2&campus=<campus>&building=<building>
```

选择房间后查询计量信息：

```text
feeitemid=409&type=IEC&level=3&campus=<campus>&building=<building>&room=<room>
```

最终响应中的 `map.data.comAddress` 是充值记录接口需要的通信地址，可能为空。为空时不能查询该校表的历史记录。

## 宿舍购电项目信息

```http
GET /charge/feeitem/singleFeeitem?feeitemid=409
```

成功响应主要字段：

```json
{
  "code": 200,
  "msg": "success",
  "feeitem": {},
  "view": {}
}
```

页面用 `feeitem.paycost`、`page_style`、`maxmoney`、`retain_money` 等字段决定金额输入和订单模式。

## 充值记录

```http
GET /charge/feeitem/getRechargeRecord
```

查询参数：

| 参数 | 必填 | 说明 |
|---|---|---|
| `feeitemid` | 是 | 固定为当前购电项目，如 `409` |
| `date` | 是 | 日期，格式由记录类型决定；月度通常为 `YYYYMM`，年度可能为 `YYYY` |
| `campus` / `building` / `room` | 房间选择模式必填 | 学校接口值 |
| `comAddress` | 是 | `getThirdData` 最终返回的通信地址 |
| `rtype` | 首次可省略 | 记录类型；首次响应返回 `recordType` 后再回填 |
| `row` / `page` | 分页类型必填 | `recordType[].ispage == "true"` 时使用 |

页面逻辑：

1. 首次请求只带项目、房间、通信地址和日期；
2. 响应中的 `recordType` 是页签列表；
3. 每个页签字段包括 `name`、`rtype`、`param`、`ispage`；
4. `param=year` 时日期控件使用 `YYYY`，其他通常使用 `YYYY-MM`；
5. 点击页签后带 `rtype` 重新查询；
6. `ispage=true` 时增加 `row`、`page`，响应分页字段为 `total`；
7. 其余记录可能直接返回对象或数组，由页面按字段动态渲染。

预期响应结构：

```json
{
  "code": 200,
  "msg": "success",
  "recordType": [
    {
      "name": "记录类型",
      "rtype": "record-type",
      "param": "month",
      "ispage": "true"
    }
  ],
  "data": [],
  "total": 0
}
```

调研期间页面本身可以正常打开，但后续连续请求触发了 CDN 504，因此尚未取得某个账号的完整 `recordType` 和数据样例。接口路径、参数和页面调用逻辑已从 `/charge-app/static/js/app.376faa24b6c05c5ebe95.js` 确认。

### 实测结论

当前 `409 宿舍购电` 的房间查询响应中没有 `comAddress`：

```json
{
  "campus": "<campus-name>",
  "building": "<building-name>",
  "room": "<room-name>",
  "tsmAbstract": "校区#...;楼宇#...;房间#...",
  "yktmercacc": "<merchant-account>",
  "sroomid": 0
}
```

尝试不带 `comAddress`，或使用房间摘要、一卡通商户号、`sroomid` 代替，接口都返回：

```json
{
  "code": 500,
  "msg": "未知异常，请联系管理员"
}
```

因此房间级 `getRechargeRecord` 虽然前端存在，但当前宿舍购电项目缺少调用所需地址，不能直接作为可用数据源。

## 个人订单和账单

待缴订单：

```http
GET /charge/order/personal_data?status=0
```

历史/退款订单：

```http
GET /charge/order/oldOrdersData?status=1
GET /charge/order/goRefundData?orderid=<orderid>
```

订单详情和账单统计：

```http
GET /charge/turnover/appAccountDetail?turnoverid=<turnoverid>
GET /charge/turnover/app_totalAccount?feeitemid=409&appId=34
GET /charge/turnover/app_account
GET /charge/turnover/mouthAccount
GET /charge/turnover/pie_account
```

`app_totalAccount` 的已测响应：

```json
{
  "code": 200,
  "msg": "success",
  "accountTotal": 123.45
}
```

`accountTotal` 是账户汇总，不是某个宿舍的剩余电费。

### 个人充值订单结构

`status=0` 和 `status=1` 是当前可用的真实充值/缴费数据源。一次实测结果：

| 请求 | 返回列表 | 数量 |
|---|---|---|
| `/charge/order/personal_data?status=0` | `orderList` | 待支付订单 |
| `/charge/order/personal_data?status=1` | `orderList` | 已支付订单 |
| `/charge/order/oldOrdersData?status=1` | `oldList` | 已支付历史订单，与上一项基本一致 |

单条订单的可用字段：

```json
{
  "orderid": "<orderid>",
  "tranamt": 10,
  "actulamt": 10,
  "status": 1,
  "payid": 65,
  "commitdate": "YYYY-MM-DD HH:MM:SS",
  "successdate": "YYYY-MM-DD HH:MM:SS",
  "payexpdate": "YYYY-MM-DD HH:MM:SS",
  "abstracts": "公寓-校区;楼层-楼栋;房间-房间",
  "third_party": {
    "campus": "<campus>",
    "building": "<building>",
    "room": "<room>",
    "remark": "{\"电表总用电量\":\"...\",\"当前剩余电量\":\"...\"}"
  },
  "orderDetailList": [
    {
      "feeitemid": 409,
      "actulamt": 10,
      "turnoverid": 0,
      "status": 1
    }
  ],
  "payBean": {
    "payid": 65,
    "name": "卡账户",
    "code": "CARDTSM",
    "nopassword": 0
  }
}
```

响应还包含 `userBean`、`userno`、`sno`、姓名和银行卡尾号等个人信息。后续 API 不得原样返回这些字段，只应返回房间、金额、时间、状态和脱敏订单号。

待支付订单也可能长期停留。实测 `status=0` 中存在几个月前的过期订单，因此网页展示时应标记为已过期，并允许管理员清理。

订单状态至少可区分：

- `status=0`：待支付，`successdate`、`actulamt`、`payid` 可能为空；
- `status=1`：已支付，包含成功时间、实际金额和支付方式。

## 金额选择

可以选金额。当前 `409 宿舍购电` 的 `singleFeeitem` 实测：

```json
{
  "feeitemid": 409,
  "name": "宿舍购电",
  "layout": "10,50,100",
  "paycost": 0,
  "maxmoney": null,
  "retain_money": null,
  "daymaxmoney": null
}
```

- 页面显示 `10 元`、`50 元`、`100 元` 三个快捷金额；
- 同时提供自定义金额输入框；
- 默认选中 `10 元`；
- `maxmoney`、`retain_money`、`daymaxmoney` 当前均为空，前端只校验金额必须为正数；
- 最终金额通过 `/blade-pay/pay` 的 `tranamt` 字段提交。

已支付订单中实际出现过 `1`、`6.58`、`9`、`10`、`30`、`50`、`100` 元，说明自定义金额确实会进入学校支付系统，不仅限于三个快捷金额。

即使学校未设置单笔或每日上限，本项目加入充值功能时仍应增加本地最大金额、每日累计额度和二次确认。

## 504 与 WAF 行为

实测 504 不是固定的“第 N 次请求”阈值。

| 场景 | 结果 |
|---|---|
| 新浏览器加载 `/charge-app/` 完整页面 | 约 14 个 HTML/CSS/JS/API 请求全部成功 |
| 同一页面再执行房间和项目查询 | 2 个请求成功 |
| 关闭页面后立刻重新打开并重复 API 探测 | 新一轮第一个房间请求即出现 504 |
| 本地并发 curl 3 个接口 | 部分 200、部分 504 |
| 从生产服务器重复查询订单接口 | 全部成功 |
| 从生产服务器串行查询 `getCurrentTime` 20 次，间隔 200ms | 20/20 成功，未触发 504 |

504 响应特征：

```http
HTTP/1.1 504 Gateway Time-out
X-JSL-Shadow-Connection: fail
X-Cache: error
```

结合请求表现，判断为知道创宇 CDN/WAF 的影子连接与请求指纹/频率判定，而不是业务接口自身的固定限流。重复使用短生命周期浏览器会话、没有完整页面 Cookie/Referer、并发直接请求接口，都会增加触发概率。

后续客户端应：

- 复用同一 charge 会话 Cookie 和 HTTP 连接；
- 每次登录后先加载真实页面或执行现有 `Establish` 流程；
- 避免并发探测多个 charge 接口；
- 对只读数据做短时缓存；
- 遇 504 不要立即重试，退避后再试；
- 不把 504 误判为 token 失效。

## 支付方式

页面预览支付方式：

```http
GET /charge/pay/getpayinfo?feeitemid=409&appId=34
```

订单创建后按订单查询：

```http
GET /charge/pay/getpayinfo?orderid=<orderid>
```

已测响应中，宿舍购电只有一种支付方式：

```json
{
  "payid": 65,
  "name": "卡账户",
  "code": "CARDTSM",
  "nopassword": 0,
  "chooseAccount": false
}
```

`CARDTSM` 表示学校校园卡/一卡通账户，`nopassword=0` 表示需要校园卡查询密码。

## 创建未支付订单

```http
POST /blade-pay/pay
Content-Type: application/x-www-form-urlencoded
```

创建订单阶段 `paystep=0`。基础字段：

```text
feeitemid=409
tranamt=10.00
flag=choose
source=app
paystep=0
abstracts=<房间/项目描述>
```

可能附加字段：

- `ids`：应收项目 ID
- `tranamt_prices`：应收项目金额
- `third_party`：电表通信信息和房间摘要
- `deductflag` / `deductids`：助学贷款抵扣

成功响应中的 `data.orderid` 会存入 `sessionStorage.orderid`，然后跳转到确认缴费页。

该接口会创建真实的待支付订单，即使没有扣款。自动测试必须在成功后调用取消接口，或只做源码解析而不实际调用。

## 获取支付密码映射

选中 `CARDTSM` 后，再请求一次：

```text
POST /blade-pay/pay
orderid=<orderid>
paytype=CARDTSM
paytypeid=65
paystep=2
```

响应中的关键字段：

```json
{
  "data": {
    "passwordMap": {
      "<uuid>": ["3", "8", "1", "9", "6", "2", "7", "0", "5", "4"]
    }
  }
}
```

这是支付安全键盘：

1. 服务端随机排列 `0-9`；
2. 页面按数组顺序显示数字；
3. 用户点击真实密码对应的数字；
4. 前端提交的是被点击项的数组索引，不是数字本身；
5. 同时提交本次 `uuid`，服务端按映射还原真实密码。

例如页面显示 `3 8 1 9 6 2 7 0 5 4`，密码为 `602`，提交的位置索引是 `452`。

## 最终支付

```http
POST /blade-pay/pay
Content-Type: application/x-www-form-urlencoded
```

支付阶段字段：

```text
orderid=<orderid>
paystep=2
paytype=CARDTSM
paytypeid=65
password=<6位位置索引>
uuid=<passwordMap uuid>
isWX=0
```

如果订单包含账户选择，还可能带：

- `accountno`
- `ccctype`

支付成功后页面跳转到 `/charge/#/payResult` 或第三方返回地址。

### 1 元实付验证

使用 `CARDTSM` 和 6 位校园卡查询密码完成过一次真实 1 元充值：

- `/blade-pay/pay` 返回 HTTP 200、`code=200`、`msg=操作成功`；
- 紧接的订单查询仍可能暂时显示 `status=0`；
- 约 5 秒后订单进入 `status=1`，`actulamt=1`，并生成 `successdate`；
- 支付方式为 `payid=65 / CARDTSM`；
- 账户汇总金额增加 1 元；
- 电表 `当前剩余电量` 没有立即变化，至少 1 分钟内仍未到账，说明购电和电表读数之间存在异步同步。

因此客户端不能把“支付接口返回 200”直接等同于“余额已到账”，应轮询订单状态，并在之后继续观察房间余额。

## 取消订单

```http
POST /charge/order/deleteOrder
```

参数或表单：

```text
orderid=<orderid>
```

只应允许取消 `status=0` 且未过期的待支付订单。

## 当前浏览器代理实现

`/user` 不把学校账号密码或校园卡支付密码保存到服务端：

1. 浏览器先请求 `/api/user/login/keyboard`，拿到本次随机安全键盘；
2. 浏览器在本地转换学校密码，只把映射值和短时 `session_id` 提交给 `/api/user/login`；
3. 登录返回的学校 token 使用浏览器 AES-GCM 保存到 IndexedDB，服务端不写 `token.json`；
4. 如果登录账号与服务器监控账号相同，学校会把旧设备会话标记为“已经在其他设备登录”；页面要求二次确认，并允许用管理密钥将浏览器新 token 显式同步给服务器；
5. 查询房间、订单和创建待支付订单时，浏览器通过 `X-School-Token` 把 token 传给受限代理；
6. 支付准备接口返回随机数字数组，浏览器把用户点击的六位密码转换成位置索引；
7. 最终支付只提交 `password_indexes`、`uuid` 和订单号，服务端不接收明文支付密码。

安全措施：

- 服务端按 token 哈希复用 15 分钟内的 charge 会话；
- 同监控账号登录必须在页面二次确认，避免无提示踢掉服务器会话；
- 浏览器 token 可在提供管理密钥后显式同步到服务器，恢复监控采集；
- 用户查询使用独立限流器，支付操作使用更严格的独立限流器；
- 创建订单后校验房间、金额、订单状态和 `CARDTSM` 支付方式，不一致立即取消；
- 支付密码映射领取一次即从服务端内存删除，禁止自动重试最终扣款；
- 房间和订单响应只返回展示所需字段，不返回姓名、学号、银行卡尾号等个人信息；
- 浏览器缓存只保存脱敏房间和订单数据，退出登录时一并清除；
- `X-School-Token` 和 `/api/user/` 永不进入 Service Worker 缓存。

当前限制：

- 服务端只限制单笔金额 `1..500` 元，没有跨订单的每日累计额度；
- 学校接口的 504 不是固定请求次数阈值，短生命周期会话、并发探测和指纹异常都会触发；
- 页面缓存和代理会话只在单个浏览器/进程生命周期内有效，服务重启后需要重新登录；
- 学校同账号仅保留一个设备会话；在其他设备重新登录后，服务器旧 token 会返回 `401 已经在其他设备登录`，文件上的有效期仍可能显示未到期；
- 支付结果和电表到账是异步的，支付接口返回成功不能直接推断余额已经变化；
- 本地 IndexedDB 加密不能替代同源 XSS 防护；不要在不信任的页面环境输入密码。

## 集成范围

### 已实现

- 复用 charge `Establish`、cookie jar 和会话；
- 查询房间余额、校区/楼栋/房间选项；
- 查询 `personal_data?status=0` 待支付订单和 `status=1` 已支付订单；
- 订单只展示金额、状态、时间、房间和订单尾号，不返回学校响应中的个人信息；
- 不自动复用历史未支付订单，避免金额/房间混淆；
- 创建待支付订单前重新查询房间，创建后校验房间、金额、状态和支付方式；
- 获取本次随机支付键盘，浏览器提交六位位置索引；
- 支付映射一次性使用，最终扣款和取消请求均不自动重试；
- 用户可手动取消待支付订单。

### 尚未实现

充值属于有资金风险操作，应满足：

- 每日累计额度或学校侧支付额度预检；
- 待支付订单的持久化超时自动取消任务；
- 面向运维人员的订单审计留存和人工处置界面；
- 学校 `getRechargeRecord` 房间级记录。当前 `409` 项目缺少可用的 `comAddress`，因此只使用个人订单数据。

当前实现保留单笔金额限制、独立限流、两段式确认和一次性支付映射。生产环境还应根据学校授权和资金风险补充每日累计额度、审计留存和人工处置流程。
