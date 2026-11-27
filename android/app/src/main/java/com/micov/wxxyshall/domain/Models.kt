package com.micov.wxxyshall.domain

import kotlinx.serialization.Serializable

const val DEFAULT_BASE_URL = "https://wxxyshall.usts.edu.cn"
const val DEFAULT_FEE_ITEM_ID = 409
const val DEFAULT_APP_ID = 34
const val DEFAULT_RATE_LIMIT_PER_MINUTE = 30
const val MIN_POLL_INTERVAL_MINUTES = 15
const val DEFAULT_POLL_INTERVAL_MINUTES = 60
const val DEFAULT_LOW_BALANCE_THRESHOLD = 10.0
const val DEFAULT_DAILY_NOTIFY_TIME = "08:00"
const val MIN_RECHARGE_AMOUNT = 1.0
const val MAX_RECHARGE_AMOUNT = 500.0

data class SchoolSession(
    val accessToken: String,
    val refreshToken: String = "",
    val expiresIn: Int,
    val loginTime: Long,
    val sno: String,
) {
    fun expiresAtSeconds(): Long = loginTime + expiresIn

    fun secondsLeft(nowSeconds: Long = System.currentTimeMillis() / 1000): Long =
        (expiresAtSeconds() - nowSeconds).coerceAtLeast(0)

    fun isExpired(
        bufferSeconds: Long = 600,
        nowSeconds: Long = System.currentTimeMillis() / 1000,
    ): Boolean = expiresIn > 0 && expiresAtSeconds() - bufferSeconds <= nowSeconds
}

@Serializable
data class Option(val value: String, val name: String)

data class Reading(
    val surplusCharge: Double?,
    val show: Map<String, String>,
    val raw: Map<String, String>,
    val epoch: Long = System.currentTimeMillis() / 1000,
) {
    val totalUsage: Double?
        get() = show["电表总用电量"]?.trim()?.toDoubleOrNull()
}

data class RechargeOrder(
    val orderId: String,
    val tranAmount: Double,
    val actualAmount: Double?,
    val status: Int,
    val commitDate: String,
    val successDate: String,
    val payExpireDate: String,
    val payId: Int?,
    val abstracts: String,
    val feeItemId: Int?,
    val room: OrderRoom?,
)

data class OrderRoom(
    val campus: String,
    val building: String,
    val room: String,
    val campusId: String = "",
    val buildingId: String = "",
    val roomId: String = "",
)

data class PaymentMethod(
    val payId: Int,
    val name: String,
    val code: String,
    val noPassword: Int,
    val chooseAccount: Boolean,
)

data class PasswordMap(val uuid: String, val keys: List<Int>)

data class RoomSnapshot(
    val reading: Reading,
    val rawMap: String,
    val campusName: String,
    val buildingName: String,
    val roomName: String,
)

enum class PaymentStage {
    CREATED,
    PREPARED,
    SUBMITTING,
    SUBMITTED,
    UNKNOWN,
}

data class PendingPayment(
    val orderId: String,
    val targetKey: String,
    val roomLabel: String,
    val amount: Double,
    val method: PaymentMethod,
    val stage: PaymentStage,
    val expiresAt: Long,
    val passwordMap: PasswordMap? = null,
)

enum class NotificationMode(val wireValue: String) {
    NONE("none"),
    ALERT("alert"),
    DAILY("daily");

    companion object {
        fun fromWire(value: String?): NotificationMode =
            entries.firstOrNull { it.wireValue == value } ?: ALERT
    }
}

data class Target(
    val key: String,
    val feeItemId: Int = DEFAULT_FEE_ITEM_ID,
    val appId: Int = DEFAULT_APP_ID,
    val campus: String,
    val building: String,
    val room: String,
    val label: String,
    val pollIntervalMinutes: Int = DEFAULT_POLL_INTERVAL_MINUTES,
    val notifyMode: NotificationMode = NotificationMode.ALERT,
    val notifyTime: String = DEFAULT_DAILY_NOTIFY_TIME,
    val lowBalanceThreshold: Double = DEFAULT_LOW_BALANCE_THRESHOLD,
    val nextDueAt: Long = 0,
    val createdAt: Long = System.currentTimeMillis(),
) {
    companion object {
        fun key(campus: String, building: String, room: String): String =
            "$campus|$building|$room"
    }
}

data class TargetDraft(
    val campus: String,
    val building: String,
    val room: String,
    val label: String,
    val pollIntervalMinutes: Int = DEFAULT_POLL_INTERVAL_MINUTES,
    val notifyMode: NotificationMode = NotificationMode.ALERT,
    val notifyTime: String = DEFAULT_DAILY_NOTIFY_TIME,
    val lowBalanceThreshold: Double = DEFAULT_LOW_BALANCE_THRESHOLD,
)

enum class SchoolErrorKind {
    CREDENTIALS,
    CHALLENGE,
    AUTH,
    UNAVAILABLE,
    BAD_RESPONSE,
    PAYMENT_UNKNOWN,
}

class SchoolException(
    val kind: SchoolErrorKind,
    override val message: String,
    cause: Throwable? = null,
) : Exception(message, cause)
