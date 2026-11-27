# Android 客户端

纯本地 Android 客户端，直接调用学校接口完成登录、宿舍余额采集、历史记录、原生通知、订单查询和校园卡充值，不依赖本项目的 Go 服务。

开发进度、验收结果、签名 key 状态和已知风险见
[`docs/ANDROID_CLIENT_PROGRESS.md`](../docs/ANDROID_CLIENT_PROGRESS.md)。

## 环境

- JDK 17
- Android SDK Platform 35
- Android Build Tools 35.0.0
- Android 8.0（API 26）或更高版本的设备

本工程通过 Gradle Wrapper 固定 Gradle 8.14.4。`local.properties` 和签名文件已被 Git 忽略。

```bash
export JAVA_HOME=/path/to/jdk-17
export ANDROID_HOME=/opt/android-sdk
export PATH="$ANDROID_HOME/platform-tools:$PATH"
cd android
./gradlew testDebugUnitTest lintDebug assembleDebug
```

Debug APK 位于：

```text
app/build/outputs/apk/debug/app-debug.apk
```

安装到通过 ADB 连接的设备：

```bash
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

如需通过本地代理下载 Gradle 依赖：

```bash
JAVA_TOOL_OPTIONS='-Dhttp.proxyHost=127.0.0.1 -Dhttp.proxyPort=7890 -Dhttps.proxyHost=127.0.0.1 -Dhttps.proxyPort=7890' \
  ./gradlew assembleDebug
```

## 正式签名

Release APK 只有在以下四个环境变量均存在时才会签名：

```text
ANDROID_KEYSTORE_FILE
ANDROID_KEYSTORE_PASSWORD
ANDROID_KEY_ALIAS
ANDROID_KEY_PASSWORD
```

签名私钥不得提交到仓库：

```bash
export ANDROID_KEYSTORE_FILE=/secure/path/wxxyshall-release.jks
export ANDROID_KEYSTORE_PASSWORD='...'
export ANDROID_KEY_ALIAS='...'
export ANDROID_KEY_PASSWORD='...'
./gradlew assembleRelease
```

## 运行约束

- Android WorkManager 周期任务最短为 15 分钟，并受 Doze、厂商省电策略和强制停止影响；它不是精确定时器。
- 学校同一账号只保留一个设备会话。Android 登录会使其他设备或服务器上的旧会话失效。
- 明文学校密码不落盘。学校 token 使用 Android Keystore 加密；充值页禁止截屏，进入和最终支付前要求生物识别或设备凭据。
- 校园卡支付请求不会自动重试。网络异常时应用会显示“支付结果待确认”，必须先查询学校订单和宿舍余额。
