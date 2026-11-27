package com.micov.wxxyshall.data.remote

import com.micov.wxxyshall.domain.DEFAULT_APP_ID
import com.micov.wxxyshall.domain.DEFAULT_BASE_URL
import com.micov.wxxyshall.domain.DEFAULT_FEE_ITEM_ID
import com.micov.wxxyshall.domain.Option
import com.micov.wxxyshall.domain.OrderRoom
import com.micov.wxxyshall.domain.PasswordMap
import com.micov.wxxyshall.domain.PaymentMethod
import com.micov.wxxyshall.domain.Reading
import com.micov.wxxyshall.domain.RechargeOrder
import com.micov.wxxyshall.domain.RoomSnapshot
import com.micov.wxxyshall.domain.SchoolErrorKind
import com.micov.wxxyshall.domain.SchoolException
import java.io.IOException
import java.util.concurrent.TimeUnit
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.doubleOrNull
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.FormBody
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody

private const val CHARGE_BASIC = "Basic Y2hhcmdlOmNoYXJnZV9zZWNyZXQ="
private const val CHARGE_USER_AGENT =
    "Mozilla/5.0 (Linux; Android 13; Pixel 5) AppleWebKit/537.36 " +
        "(KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36"
private const val MAX_CHARGE_RESPONSE_BYTES = 8 * 1024 * 1024
private val AUTH_CODES = setOf(401, 403, 1001)

class SchoolChargeClient(
    baseUrl: String = DEFAULT_BASE_URL,
    private val token: String,
    private val pacer: RequestPacer,
    private val client: OkHttpClient = defaultClient(),
    private val json: Json = Json { ignoreUnknownKeys = true },
) {
    private val baseUrl = baseUrl.trimEnd('/')
    private val establishMutex = Mutex()
    @Volatile private var established = false

    suspend fun listCampuses(feeItemId: Int = DEFAULT_FEE_ITEM_ID): List<Option> {
        ensureEstablished(feeItemId, DEFAULT_APP_ID)
        return readOptions(feeItemId, level = 0)
    }

    suspend fun listBuildings(
        campus: String,
        feeItemId: Int = DEFAULT_FEE_ITEM_ID,
    ): List<Option> {
        ensureEstablished(feeItemId, DEFAULT_APP_ID)
        return readOptions(feeItemId, level = 1, campus = campus)
    }

    suspend fun listRooms(
        campus: String,
        building: String,
        feeItemId: Int = DEFAULT_FEE_ITEM_ID,
    ): List<Option> {
        ensureEstablished(feeItemId, DEFAULT_APP_ID)
        return readOptions(feeItemId, level = 2, campus = campus, building = building)
    }

    suspend fun queryRoomSnapshot(
        campus: String,
        building: String,
        room: String,
        feeItemId: Int = DEFAULT_FEE_ITEM_ID,
    ): RoomSnapshot {
        ensureEstablished(feeItemId, DEFAULT_APP_ID)
        val map = thirdData(
            feeItemId = feeItemId,
            form = form(
                "feeitemid" to feeItemId.toString(),
                "type" to "IEC",
                "level" to "3",
                "campus" to campus,
                "building" to building,
                "room" to room,
            ),
        )
        val data = map["data"].decodeObject()
        return RoomSnapshot(
            reading = parseReading(map),
            rawMap = map.toString(),
            campusName = data?.string("campus").orEmpty(),
            buildingName = data?.string("building").orEmpty(),
            roomName = data?.string("room").orEmpty(),
        )
    }

    suspend fun listOrders(status: Int): List<RechargeOrder> {
        require(status == 0 || status == 1)
        ensureEstablished()
        val body = successJson(
            execute(
                path = "/charge/order/personal_data?status=$status",
                retryable = true,
            ),
            "/charge/order/personal_data",
        )
        return body["orderList"].decodeArray().orEmpty().mapNotNull { element ->
            runCatching { summarizeOrder(element.jsonObject) }.getOrNull()
        }
    }

    suspend fun getOrder(orderId: String): Pair<RechargeOrder, List<PaymentMethod>> {
        ensureEstablished()
        val body = successJson(
            execute(
                path = "/charge/pay/getpayinfo?orderid=${encodeQuery(orderId)}",
                retryable = true,
            ),
            "/charge/pay/getpayinfo",
        )
        val order = body["order"].decodeObject()
            ?: throw SchoolException(SchoolErrorKind.BAD_RESPONSE, "支付信息缺少 order")
        val methods = body["payList"].decodeArray().orEmpty().mapNotNull { element ->
            runCatching {
                val item = element.jsonObject
                PaymentMethod(
                    payId = item.int("payid"),
                    name = item.string("name"),
                    code = item.string("code"),
                    noPassword = item.int("nopassword"),
                    chooseAccount = item.boolean("chooseAccount"),
                )
            }.getOrNull()
        }
        return summarizeOrder(order) to methods
    }

    suspend fun createOrder(
        feeItemId: Int,
        amount: Double,
        roomRawMap: String,
    ): String {
        ensureEstablished(feeItemId, DEFAULT_APP_ID)
        val map = runCatching { json.parseToJsonElement(roomRawMap).jsonObject }.getOrElse {
            throw SchoolException(SchoolErrorKind.BAD_RESPONSE, "房间信息无效")
        }
        val room = map["data"].decodeObject()
            ?: throw SchoolException(SchoolErrorKind.BAD_RESPONSE, "房间信息缺少 data")
        val thirdParty = room.toMutableMap().apply {
            val campus = room.string("campus")
            val building = room.string("building")
            val roomName = room.string("room")
            put("myCustomInfo", JsonPrimitive("房间：$campus $building $roomName"))
        }
        val thirdPartyJson = JsonObject(thirdParty).toString()
        val abstracts = "公寓-${room.string("campus")};楼层-${room.string("building")};房间-${room.string("room")}"
        val body = successJson(
            execute(
                path = "/blade-pay/pay",
                retryable = false,
                paymentUnknownOnFailure = true,
                form = form(
                    "feeitemid" to feeItemId.toString(),
                    "tranamt" to String.format(java.util.Locale.US, "%.2f", amount),
                    "flag" to "choose",
                    "source" to "app",
                    "paystep" to "0",
                    "abstracts" to abstracts,
                    "third_party" to thirdPartyJson,
                ),
            ),
            "/blade-pay/pay",
        )
        val orderId = body["data"].decodeObject()?.string("orderid").orEmpty()
        if (orderId.isBlank()) {
            throw SchoolException(SchoolErrorKind.BAD_RESPONSE, "创建订单响应缺少 orderid")
        }
        return orderId
    }

    suspend fun getPasswordMap(orderId: String, method: PaymentMethod): PasswordMap {
        ensureEstablished()
        val body = successJson(
            execute(
                path = "/blade-pay/pay",
                retryable = false,
                form = form(
                    "orderid" to orderId,
                    "paytype" to method.code,
                    "paytypeid" to method.payId.toString(),
                    "paystep" to "2",
                ),
            ),
            "/blade-pay/pay",
        )
        val maps = body["data"].decodeObject()?.get("passwordMap").decodeObject()
            ?: throw SchoolException(SchoolErrorKind.BAD_RESPONSE, "支付响应缺少 passwordMap")
        if (maps.size != 1) {
            throw SchoolException(SchoolErrorKind.BAD_RESPONSE, "支付安全键盘数量异常")
        }
        val entry = maps.entries.first()
        val keys = entry.value.decodeKeys().orEmpty()
        if (keys.size != 10 || keys.toSet() != (0..9).toSet()) {
            throw SchoolException(SchoolErrorKind.BAD_RESPONSE, "支付安全键盘映射不完整")
        }
        return PasswordMap(uuid = entry.key, keys = keys)
    }

    suspend fun submitPassword(
        orderId: String,
        method: PaymentMethod,
        passwordIndexes: String,
        uuid: String,
    ): JsonObject {
        if (!passwordIndexes.matches(Regex("\\d{6}")) || uuid.isBlank()) {
            throw SchoolException(SchoolErrorKind.BAD_RESPONSE, "支付密码映射无效")
        }
        ensureEstablished()
        val body = successJson(
            execute(
                path = "/blade-pay/pay",
                retryable = false,
                form = form(
                    "orderid" to orderId,
                    "paystep" to "2",
                    "paytype" to method.code,
                    "paytypeid" to method.payId.toString(),
                    "password" to passwordIndexes,
                    "uuid" to uuid,
                    "isWX" to "0",
                    "userAgent" to "h5",
                ),
            ),
            "/blade-pay/pay",
            unknownPaymentOnNetworkFailure = true,
        )
        return body
    }

    suspend fun cancelOrder(orderId: String) {
        ensureEstablished()
        val body = JsonObject(mapOf("orderid" to JsonPrimitive(orderId)))
            .toString()
            .toRequestBody(JSON_CONTENT_TYPE)
        successJson(
            execute(
                path = "/charge/order/deleteOrder",
                retryable = false,
                form = body,
            ),
            "/charge/order/deleteOrder",
        )
    }

    private suspend fun readOptions(
        feeItemId: Int,
        level: Int,
        campus: String = "",
        building: String = "",
    ): List<Option> {
        val values = buildList {
            add("feeitemid" to feeItemId.toString())
            add("type" to "select")
            add("level" to level.toString())
            if (campus.isNotBlank()) add("campus" to campus)
            if (building.isNotBlank()) add("building" to building)
        }
        val map = thirdData(feeItemId, form(*values.toTypedArray()))
        return map["data"].decodeArray().orEmpty().mapNotNull { element ->
            runCatching {
                val item = element.jsonObject
                val value = item.string("value")
                val name = item.string("name")
                if (value.isBlank() || name.isBlank()) null else Option(value, name)
            }.getOrNull()
        }
    }

    private suspend fun thirdData(feeItemId: Int, form: RequestBody): JsonObject {
        ensureEstablished(feeItemId, DEFAULT_APP_ID)
        val body = successJson(
            execute(
                path = "/charge/feeitem/getThirdData",
                retryable = true,
                form = form,
            ),
            "/charge/feeitem/getThirdData",
        )
        return body["map"].decodeObject()
            ?: throw SchoolException(SchoolErrorKind.BAD_RESPONSE, "学校接口缺少 map 字段")
    }

    private suspend fun ensureEstablished(
        feeItemId: Int = DEFAULT_FEE_ITEM_ID,
        appId: Int = DEFAULT_APP_ID,
    ) {
        if (established) return
        establishMutex.withLock {
            if (established) return
            execute(
                path = "/charge/feeitem/toAppitem?feeitemid=$feeItemId&appId=$appId" +
                    "&loginFrom=h5&synjones-auth=${encodeQuery(token)}",
                retryable = true,
            )
            execute(path = "/charge/user/caslogin_userxx", retryable = true)
            established = true
        }
    }

    private suspend fun execute(
        path: String,
        retryable: Boolean,
        paymentUnknownOnFailure: Boolean = false,
        form: RequestBody? = null,
    ): String = withContext(Dispatchers.IO) {
        var lastError: SchoolException? = null
        val attempts = if (retryable) 3 else 1
        repeat(attempts) { attempt ->
            val request = requestBuilder(path, form, if (form == null) "GET" else "POST").build()
            try {
                executeFollowingRedirects(request).use { response ->
                    if (response.code == 401 || response.code == 403) {
                        throw SchoolException(SchoolErrorKind.AUTH, "学校登录已失效，请重新登录")
                    }
                    val body = readLimited(response.body?.string().orEmpty())
                    if (response.code in setOf(408, 429, 500, 502, 503, 504) && retryable) {
                        lastError = SchoolException(
                            SchoolErrorKind.UNAVAILABLE,
                            "学校接口 HTTP ${response.code}",
                        )
                        return@use
                    }
                    if (!response.isSuccessful) {
                        val message = runCatching {
                            json.parseToJsonElement(body).jsonObject.string("msg")
                        }.getOrDefault("")
                        if (paymentUnknownOnFailure) {
                            throw SchoolException(
                                SchoolErrorKind.PAYMENT_UNKNOWN,
                                "支付结果未知，请先核对订单，不要重复提交",
                            )
                        }
                        throw SchoolException(
                            SchoolErrorKind.UNAVAILABLE,
                            message.ifBlank { "学校接口 HTTP ${response.code}" },
                        )
                    }
                    return@withContext body
                }
            } catch (error: SchoolException) {
                if (error.kind == SchoolErrorKind.AUTH || !retryable) throw error
                lastError = error
            } catch (error: IOException) {
                if (paymentUnknownOnFailure) {
                    throw SchoolException(
                        SchoolErrorKind.PAYMENT_UNKNOWN,
                        "支付结果未知，请先核对订单，不要重复提交",
                        error,
                    )
                }
                val mapped = SchoolException(
                    SchoolErrorKind.UNAVAILABLE,
                    "网络错误: ${error.message.orEmpty()}",
                    error,
                )
                if (!retryable) throw mapped
                lastError = mapped
            }
            if (attempt < attempts - 1) {
                kotlinx.coroutines.delay(1_500L * (attempt + 1))
            }
        }
        throw lastError ?: SchoolException(SchoolErrorKind.UNAVAILABLE, "学校请求失败")
    }

    private suspend fun executeFollowingRedirects(initial: Request): okhttp3.Response {
        var request = initial
        repeat(6) {
            pacer.await()
            val response = client.newCall(request).execute()
            if (!response.isRedirect) return response
            val next = runCatching {
                request.url.resolve(response.header("Location").orEmpty())
            }.getOrNull()
            val method = request.method
            val body = request.body
            response.close()
            if (next == null) {
                throw SchoolException(SchoolErrorKind.UNAVAILABLE, "学校重定向地址无效")
            }
            if (next.scheme != request.url.scheme || next.host != request.url.host) {
                throw SchoolException(SchoolErrorKind.UNAVAILABLE, "拒绝跨源重定向")
            }
            request = request.newBuilder().url(next).apply {
                when {
                    response.code == 307 || response.code == 308 -> method(method, body)
                    else -> get()
                }
            }.build()
        }
        throw SchoolException(SchoolErrorKind.UNAVAILABLE, "学校重定向次数过多")
    }

    private fun requestBuilder(
        path: String,
        form: RequestBody?,
        method: String,
    ): Request.Builder {
        val builder = Request.Builder()
            .url(baseUrl + path)
            .header("User-Agent", CHARGE_USER_AGENT)
            .header("Accept", "application/json, text/plain, */*")
            .header("Authorization", CHARGE_BASIC)
            .header("synjones-auth", "bearer $token")
        if (method == "POST" && form != null) {
            builder.post(form)
        }
        return builder
    }

    private fun successJson(
        raw: String,
        path: String,
        unknownPaymentOnNetworkFailure: Boolean = false,
    ): JsonObject {
        val root = runCatching { json.parseToJsonElement(raw).jsonObject }.getOrElse {
            if (unknownPaymentOnNetworkFailure) {
                throw SchoolException(
                    SchoolErrorKind.PAYMENT_UNKNOWN,
                    "支付结果未知，请先核对订单，不要重复提交",
                    it,
                )
            }
            throw SchoolException(SchoolErrorKind.AUTH, "学校返回非 JSON($path)，疑似登录失效")
        }
        val code = root.int("code")
        if (code in AUTH_CODES) {
            throw SchoolException(SchoolErrorKind.AUTH, "学校登录已失效，请重新登录")
        }
        if (code != 200) {
            throw SchoolException(
                SchoolErrorKind.UNAVAILABLE,
                root.string("msg").ifBlank { "学校接口返回 code=$code($path)" },
            )
        }
        return root
    }

    private fun parseReading(map: JsonObject): Reading {
        val show = map["showData"].decodeObject()?.mapValues { entry ->
            runCatching { entry.value.jsonPrimitive.contentOrNull }
                .getOrNull()
                ?: entry.value.toString()
        }.orEmpty()
        val raw = map["data"].decodeObject()?.mapValues { entry ->
            runCatching { entry.value.jsonPrimitive.contentOrNull }
                .getOrNull()
                ?: entry.value.toString()
        }.orEmpty()
        return Reading(
            surplusCharge = map["surplusCharge"]?.jsonPrimitive?.doubleOrNull
                ?: map.string("surplusCharge").toDoubleOrNull(),
            show = show,
            raw = raw,
        )
    }

    private fun summarizeOrder(raw: JsonObject): RechargeOrder {
        val thirdParty = when (val value = raw["third_party"]) {
            is JsonObject -> value
            is JsonPrimitive -> runCatching {
                json.parseToJsonElement(value.content).jsonObject
            }.getOrNull()
            else -> null
        }
        val room = thirdParty?.let {
            OrderRoom(
                campus = it.string("campus"),
                building = it.string("building"),
                room = it.string("room"),
                campusId = it.string("campusid"),
                buildingId = it.string("buildingid"),
                roomId = it.string("roomid"),
            )
        }
        val feeItemId = raw["feeitemlist"].decodeArray()?.firstOrNull()
            ?.jsonObject?.int("feeitemid")
        return RechargeOrder(
            orderId = raw.string("orderid"),
            tranAmount = raw.double("tranamt"),
            actualAmount = raw["actulamt"]?.jsonPrimitive?.doubleOrNull,
            status = raw.int("status"),
            commitDate = raw.string("commitdate"),
            successDate = raw.string("successdate"),
            payExpireDate = raw.string("payexpdate"),
            payId = raw["payid"]?.jsonPrimitive?.intOrNull,
            abstracts = raw.string("abstracts"),
            feeItemId = feeItemId,
            room = room,
        )
    }

    private fun JsonObject.string(key: String): String =
        this[key]?.jsonPrimitive?.contentOrNull.orEmpty().trim()

    private fun JsonObject.int(key: String): Int =
        this[key]?.jsonPrimitive?.intOrNull
            ?: this[key]?.jsonPrimitive?.contentOrNull?.toIntOrNull()
            ?: 0

    private fun JsonObject.double(key: String): Double =
        this[key]?.jsonPrimitive?.doubleOrNull
            ?: this[key]?.jsonPrimitive?.contentOrNull?.toDoubleOrNull()
            ?: 0.0

    private fun JsonObject.boolean(key: String): Boolean =
        this[key]?.jsonPrimitive?.booleanOrNull
            ?: this[key]?.jsonPrimitive?.contentOrNull?.let { it == "1" || it.equals("true", true) }
            ?: false

    private fun form(vararg values: Pair<String, String>): FormBody =
        FormBody.Builder().apply {
            values.forEach { (key, value) -> add(key, value) }
        }.build()

    private fun encodeQuery(value: String): String =
        java.net.URLEncoder.encode(value, Charsets.UTF_8.name())

    private fun readLimited(body: String): String {
        if (body.toByteArray().size > MAX_CHARGE_RESPONSE_BYTES) {
            throw SchoolException(SchoolErrorKind.BAD_RESPONSE, "学校响应超过 8 MiB")
        }
        return body
    }

    private fun JsonElement?.decodeArray(): JsonArray? = when (this) {
        is JsonArray -> this
        is JsonPrimitive -> runCatching {
            json.parseToJsonElement(content).jsonArray
        }.getOrNull()
        else -> null
    }

    private fun JsonElement?.decodeKeys(): List<Int>? {
        decodeArray()?.let { array ->
            return array.map { it.jsonPrimitive.intOrNull ?: -1 }
        }
        val text = (this as? JsonPrimitive)?.contentOrNull
            ?.trim()
            ?.removePrefix("[")
            ?.removeSuffix("]")
            ?.replace(",", "")
            ?.replace(" ", "")
            .orEmpty()
        return text.takeIf { it.length == 10 && it.all(Char::isDigit) }
            ?.map { it.digitToInt() }
    }

    private fun JsonElement?.decodeObject(): JsonObject? = when (this) {
        is JsonObject -> this
        is JsonPrimitive -> runCatching {
            json.parseToJsonElement(content).jsonObject
        }.getOrNull()
        else -> null
    }

    companion object {
        private val JSON_CONTENT_TYPE = "application/json; charset=UTF-8".toMediaType()

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

private fun List<JsonElement>?.orEmpty(): List<JsonElement> = this ?: emptyList()
