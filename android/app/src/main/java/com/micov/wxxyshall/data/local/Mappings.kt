package com.micov.wxxyshall.data.local

import com.micov.wxxyshall.domain.NotificationMode
import com.micov.wxxyshall.domain.OrderRoom
import com.micov.wxxyshall.domain.PaymentMethod
import com.micov.wxxyshall.domain.PaymentStage
import com.micov.wxxyshall.domain.PendingPayment
import com.micov.wxxyshall.domain.Reading
import com.micov.wxxyshall.domain.Target
import kotlinx.serialization.json.Json

private val json = Json { ignoreUnknownKeys = true }

fun TargetEntity.toDomain(): Target = Target(
    key = key,
    feeItemId = feeItemId,
    appId = appId,
    campus = campus,
    building = building,
    room = room,
    label = label,
    pollIntervalMinutes = pollIntervalMinutes,
    notifyMode = NotificationMode.fromWire(notifyMode),
    notifyTime = notifyTime,
    lowBalanceThreshold = lowBalanceThreshold,
    nextDueAt = nextDueAt,
    createdAt = createdAt,
)

fun Target.toEntity(): TargetEntity = TargetEntity(
    key = key,
    feeItemId = feeItemId,
    appId = appId,
    campus = campus,
    building = building,
    room = room,
    label = label,
    pollIntervalMinutes = pollIntervalMinutes,
    notifyMode = notifyMode.wireValue,
    notifyTime = notifyTime,
    lowBalanceThreshold = lowBalanceThreshold,
    nextDueAt = nextDueAt,
    createdAt = createdAt,
)

fun ReadingEntity.toDomain(): Reading {
    val show = runCatching {
        json.decodeFromString<Map<String, String>>(showJson)
    }.getOrDefault(emptyMap())
    val raw = runCatching {
        json.decodeFromString<Map<String, String>>(rawJson)
    }.getOrDefault(emptyMap())
    return Reading(
        surplusCharge = surplusCharge,
        show = show,
        raw = raw,
        epoch = epoch,
    )
}

fun PendingPaymentEntity.toDomain(): PendingPayment = PendingPayment(
    orderId = orderId,
    targetKey = targetKey,
    roomLabel = roomLabel,
    amount = amount,
    method = PaymentMethod(
        payId = payId,
        name = methodName,
        code = methodCode,
        noPassword = methodNoPassword,
        chooseAccount = methodChooseAccount,
    ),
    stage = runCatching { PaymentStage.valueOf(stage) }.getOrDefault(PaymentStage.CREATED),
    expiresAt = expiresAt,
)

fun PendingPayment.toEntity(createdAt: Long = System.currentTimeMillis()): PendingPaymentEntity =
    PendingPaymentEntity(
        orderId = orderId,
        targetKey = targetKey,
        roomLabel = roomLabel,
        amount = amount,
        payId = method.payId,
        methodName = method.name,
        methodCode = method.code,
        methodNoPassword = method.noPassword,
        methodChooseAccount = method.chooseAccount,
        stage = stage.name,
        expiresAt = expiresAt,
        createdAt = createdAt,
    )

data class OrderRoomKeys(
    val campus: String,
    val building: String,
    val room: String,
)

fun OrderRoom.roomKeys(): OrderRoomKeys = OrderRoomKeys(campus, building, room)
