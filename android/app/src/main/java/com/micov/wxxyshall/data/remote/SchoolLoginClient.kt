package com.micov.wxxyshall.data.remote

import com.micov.wxxyshall.domain.DEFAULT_BASE_URL
import com.micov.wxxyshall.domain.SchoolErrorKind
import com.micov.wxxyshall.domain.SchoolException
import com.micov.wxxyshall.domain.SchoolSession
import java.io.IOException
import java.util.concurrent.TimeUnit
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.FormBody
import okhttp3.OkHttpClient
import okhttp3.Request

private const val MAX_RESPONSE_BYTES = 256 * 1024
private const val OAUTH_BASIC =
    "Basic bW9iaWxlX3NlcnZpY2VfcGxhdGZvcm06bW9iaWxlX3NlcnZpY2VfcGxhdGZvcm1fc2VjcmV0"
private const val DESKTOP_USER_AGENT =
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
        "(KHTML, like Gecko) Chrome/158.0.0.0 Safari/537.36 Edg/158.0.0.0"
private const val ACCEPT_LANGUAGE = "zh-CN,zh;q=0.9,en;q=0.8"
private const val SEC_CH_UA = "\"Chromium\";v=\"158\", \"Not.A/Brand\";v=\"24\", \"Microsoft Edge\";v=\"158\""

class SchoolLoginClient(
    baseUrl: String = DEFAULT_BASE_URL,
    private val client: OkHttpClient = defaultClient(),
    private val json: Json = Json { ignoreUnknownKeys = true },
) {
    private val baseUrl = baseUrl.trimEnd('/')

    suspend fun login(username: String, password: String): SchoolSession =
        withContext(Dispatchers.IO) {
            val account = username.trim()
            if (account.isBlank() || password.isBlank()) {
                throw SchoolException(SchoolErrorKind.CREDENTIALS, "请输入学校账号和密码")
            }
            primeSession()
            val keyboard = fetchKeyboard()
            val mappedPassword = mapKeyboardPassword(password, keyboard)
            requestToken(account, "${mappedPassword}\$1\$${keyboard.uuid}")
        }

    private fun primeSession() {
        val request = browserRequest(
            "$baseUrl/berserker-app/frontInfo?synAccessSource=h5",
        ).get().build()
        runCatching { execute(request).close() }
    }

    private fun fetchKeyboard(): KeyboardData {
        val request = browserRequest(
            "$baseUrl/berserker-secure/keyboard?type=Standard&order=0&synAccessSource=h5",
        ).get().build()
        val body = execute(request).use { response ->
            if (!response.isSuccessful) {
                throw SchoolException(
                    SchoolErrorKind.UNAVAILABLE,
                    "安全键盘接口 HTTP ${response.code}",
                )
            }
            readLimited(response.body?.string().orEmpty())
        }
        val root = parseObject(body)
        val code = root["code"]?.jsonPrimitive?.intOrNull ?: 0
        if (code != 200) {
            throw SchoolException(
                SchoolErrorKind.UNAVAILABLE,
                root.string("message").ifBlank { root.string("msg") }
                    .ifBlank { "安全键盘接口返回 code=$code" },
            )
        }
        val data = root["data"]?.jsonObject
            ?: throw SchoolException(SchoolErrorKind.BAD_RESPONSE, "安全键盘接口缺少 data")
        val keyboard = runCatching {
            KeyboardData(
                uuid = data.string("uuid"),
                numberKeyboard = data.string("numberKeyboard"),
                lowerLetterKeyboard = data.string("lowerLetterKeyboard"),
                upperLetterKeyboard = data.string("upperLetterKeyboard"),
                symbolKeyboard = data.string("symbolKeyboard"),
            )
        }.getOrElse {
            throw SchoolException(SchoolErrorKind.BAD_RESPONSE, "安全键盘数据无效")
        }
        if (keyboard.uuid.isBlank()) {
            throw SchoolException(SchoolErrorKind.BAD_RESPONSE, "安全键盘接口未返回 uuid")
        }
        return keyboard
    }

    private fun requestToken(username: String, mappedPassword: String): SchoolSession {
        val body = FormBody.Builder()
            .add("username", username)
            .add("password", mappedPassword)
            .add("logintype", "snoNew")
            .add("grant_type", "password")
            .add("scope", "all")
            .add("loginFrom", "h5")
            .add("device_token", "h5")
            .add("synAccessSource", "h5")
            .build()
        val request = browserRequest("$baseUrl/berserker-auth/oauth/token")
            .header("Authorization", OAUTH_BASIC)
            .header("Origin", baseUrl)
            .post(body)
            .build()
        val response = execute(request)
        val rawBody = response.use { readLimited(it.body?.string().orEmpty()) }
        val root = runCatching { parseObject(rawBody) }.getOrElse {
            throw SchoolException(SchoolErrorKind.BAD_RESPONSE, "登录接口返回非 JSON")
        }
        val payload = if (root.string("access_token").isBlank() && root["data"] is JsonObject) {
            root["data"]!!.jsonObject
        } else {
            root
        }
        val accessToken = payload.string("access_token").trim()
        if (response.isSuccessful && accessToken.isNotBlank()) {
            val expiresIn = payload["expires_in"]?.jsonPrimitive?.contentOrNull
                ?.trim()?.toIntOrNull() ?: 0
            if (expiresIn < 0) {
                throw SchoolException(SchoolErrorKind.BAD_RESPONSE, "token 有效期无效")
            }
            return SchoolSession(
                accessToken = accessToken,
                refreshToken = payload.string("refresh_token").trim(),
                expiresIn = expiresIn,
                loginTime = System.currentTimeMillis() / 1000,
                sno = payload.string("sno").trim().ifBlank { username },
            )
        }

        val code = payload["code"]?.jsonPrimitive?.intOrNull
            ?: payload["status"]?.jsonPrimitive?.intOrNull
            ?: 0
        val message = payload.string("error_description")
            .ifBlank { payload.string("message") }
            .ifBlank { payload.string("msg") }
            .ifBlank { "登录接口 HTTP ${response.code}" }
        when {
            code == 8000 -> throw SchoolException(SchoolErrorKind.CREDENTIALS, "学校账号或密码错误")
            code == 8001 -> throw SchoolException(
                SchoolErrorKind.CHALLENGE,
                "该账号匹配到多个学号，暂不支持自动选择",
            )
            code == 8002 || code == 8003 -> throw SchoolException(
                SchoolErrorKind.CHALLENGE,
                "学校要求验证码，请稍后重试",
            )
            response.code == 400 || response.code == 401 -> throw SchoolException(
                SchoolErrorKind.CREDENTIALS,
                message,
            )
            else -> throw SchoolException(SchoolErrorKind.UNAVAILABLE, message)
        }
    }

    private fun browserRequest(url: String): Request.Builder =
        Request.Builder()
            .url(url)
            .header("Accept", "*/*")
            .header("Accept-Language", ACCEPT_LANGUAGE)
            .header("User-Agent", DESKTOP_USER_AGENT)
            .header("synaccesssource", "h5")
            .header("Sec-CH-UA", SEC_CH_UA)
            .header("Sec-CH-UA-Mobile", "?0")
            .header("Sec-CH-UA-Platform", "\"Windows\"")
            .header("Sec-Fetch-Dest", "empty")
            .header("Sec-Fetch-Mode", "cors")
            .header("Sec-Fetch-Site", "same-origin")

    private fun execute(initial: Request): okhttp3.Response {
        var request = initial
        repeat(5) {
            val response = try {
                client.newCall(request).execute()
            } catch (error: IOException) {
                throw SchoolException(
                    SchoolErrorKind.UNAVAILABLE,
                    "学校登录服务不可用: ${error.message.orEmpty()}",
                    error,
                )
            }
            if (!response.isRedirect) return response
            val location = response.header("Location")
            response.close()
            if (location.isNullOrBlank()) {
                throw SchoolException(SchoolErrorKind.UNAVAILABLE, "登录重定向缺少地址")
            }
            val next = request.url.resolve(location)
                ?: throw SchoolException(SchoolErrorKind.UNAVAILABLE, "登录重定向地址无效")
            if (next.host != request.url.host || next.scheme != request.url.scheme) {
                throw SchoolException(SchoolErrorKind.UNAVAILABLE, "拒绝跨源登录重定向")
            }
            request = request.newBuilder().url(next).get().build()
        }
        throw SchoolException(SchoolErrorKind.UNAVAILABLE, "登录重定向次数过多")
    }

    private fun parseObject(value: String): JsonObject =
        runCatching { json.parseToJsonElement(value).jsonObject }.getOrElse {
            throw SchoolException(SchoolErrorKind.BAD_RESPONSE, "学校返回了无法解析的数据")
        }

    private fun JsonObject.string(key: String): String =
        this[key]?.jsonPrimitive?.contentOrNull.orEmpty()

    private fun readLimited(value: String): String {
        if (value.toByteArray().size > MAX_RESPONSE_BYTES) {
            throw SchoolException(SchoolErrorKind.BAD_RESPONSE, "学校响应过大")
        }
        return value
    }

    companion object {
        fun defaultClient(): OkHttpClient = OkHttpClient.Builder()
            .cookieJar(MemoryCookieJar())
            .connectTimeout(15, TimeUnit.SECONDS)
            .readTimeout(20, TimeUnit.SECONDS)
            .writeTimeout(15, TimeUnit.SECONDS)
            .callTimeout(30, TimeUnit.SECONDS)
            .followRedirects(false)
            .followSslRedirects(false)
            .build()
    }
}
