package com.micov.wxxyshall.data

import com.micov.wxxyshall.domain.MAX_RECHARGE_AMOUNT
import com.micov.wxxyshall.domain.MIN_RECHARGE_AMOUNT
import com.micov.wxxyshall.domain.PasswordMap
import com.micov.wxxyshall.domain.PaymentMethod
import com.micov.wxxyshall.domain.PaymentStage
import com.micov.wxxyshall.domain.PendingPayment
import com.micov.wxxyshall.domain.RechargeOrder
import com.micov.wxxyshall.domain.SchoolErrorKind
import com.micov.wxxyshall.domain.SchoolException
import com.micov.wxxyshall.domain.SchoolSession
import com.micov.wxxyshall.domain.Target
import com.micov.wxxyshall.data.remote.SchoolChargeClient
import com.micov.wxxyshall.security.SecureSessionStore
import kotlin.math.abs
import kotlin.math.round

class RechargeRepository(
    private val sessionStore: SecureSessionStore,
    private val pendingRepository: PendingPaymentRepository,
    private val chargeClientFactory: (SchoolSession) -> SchoolChargeClient,
) {
    suspend fun listOrders(status: Int): List<RechargeOrder> =
        client().listOrders(status)

    suspend fun createOrder(target: Target, amount: Double): PendingPayment {
        require(amount.isFinite() && amount in MIN_RECHARGE_AMOUNT..MAX_RECHARGE_AMOUNT) {
            "充值金额必须在 ${MIN_RECHARGE_AMOUNT.toInt()}..${MAX_RECHARGE_AMOUNT.toInt()} 元之间"
        }
        val roundedAmount = round(amount * 100) / 100
        val client = client()
        val snapshot = client.queryRoomSnapshot(
            campus = target.campus,
            building = target.building,
            room = target.room,
            feeItemId = target.feeItemId,
        )
        val orderId = client.createOrder(target.feeItemId, roundedAmount, snapshot.rawMap)
        try {
            val (order, methods) = client.getOrder(orderId)
            val method = cardMethod(methods)
            if (order.status != 0 ||
                abs(order.tranAmount - roundedAmount) > 0.001 ||
                !matchesRoom(order, target)
            ) {
                client.cancelOrder(orderId)
                throw SchoolException(
                    SchoolErrorKind.BAD_RESPONSE,
                    "学校返回的订单房间或金额不一致，订单已取消",
                )
            }
            val payment = PendingPayment(
                orderId = orderId,
                targetKey = target.key,
                roomLabel = target.label,
                amount = roundedAmount,
                method = method,
                stage = PaymentStage.CREATED,
                expiresAt = System.currentTimeMillis() + PAYMENT_TTL_MILLIS,
            )
            pendingRepository.save(payment)
            return payment
        } catch (error: Exception) {
            runCatching { client.cancelOrder(orderId) }
            throw error
        }
    }

    suspend fun prepare(orderId: String): PendingPayment {
        val payment = pendingRepository.get(orderId)
            ?: throw SchoolException(SchoolErrorKind.BAD_RESPONSE, "支付状态已失效")
        if (payment.expiresAt <= System.currentTimeMillis()) {
            pendingRepository.delete(orderId)
            throw SchoolException(SchoolErrorKind.BAD_RESPONSE, "支付状态已过期，请重新发起充值")
        }
        if (payment.stage == PaymentStage.SUBMITTING ||
            payment.stage == PaymentStage.SUBMITTED ||
            payment.stage == PaymentStage.UNKNOWN
        ) {
            throw SchoolException(SchoolErrorKind.BAD_RESPONSE, "订单已提交，不能再获取支付键盘")
        }
        val client = client()
        val (order, methods) = client.getOrder(orderId)
        val method = cardMethod(methods)
        if (order.status != 0 || abs(order.tranAmount - payment.amount) > 0.001 ||
            method.payId != payment.method.payId
        ) {
            throw SchoolException(SchoolErrorKind.BAD_RESPONSE, "订单状态或支付方式已变化")
        }
        val passwordMap = client.getPasswordMap(orderId, method)
        val prepared = payment.copy(
            method = method,
            stage = PaymentStage.PREPARED,
            passwordMap = passwordMap,
            expiresAt = System.currentTimeMillis() + PAYMENT_TTL_MILLIS,
        )
        pendingRepository.save(prepared.copy(passwordMap = null))
        return prepared
    }

    suspend fun submit(payment: PendingPayment, passwordIndexes: String): PendingPayment {
        if (payment.stage != PaymentStage.PREPARED || payment.passwordMap == null) {
            throw SchoolException(SchoolErrorKind.BAD_RESPONSE, "请先获取支付安全键盘")
        }
        if (!passwordIndexes.matches(Regex("\\d{6}"))) {
            throw SchoolException(SchoolErrorKind.BAD_RESPONSE, "请输入六位校园卡密码")
        }
        if (payment.expiresAt <= System.currentTimeMillis()) {
            throw SchoolException(SchoolErrorKind.BAD_RESPONSE, "支付验证已过期，请重新获取安全键盘")
        }
        pendingRepository.save(payment.copy(stage = PaymentStage.SUBMITTING, passwordMap = null))
        val client = client()
        return try {
            val (order, _) = client.getOrder(payment.orderId)
            if (order.status != 0) {
                throw SchoolException(SchoolErrorKind.BAD_RESPONSE, "订单已不是待支付状态")
            }
            client.submitPassword(
                orderId = payment.orderId,
                method = payment.method,
                passwordIndexes = passwordIndexes,
                uuid = payment.passwordMap.uuid,
            )
            val submitted = payment.copy(
                stage = PaymentStage.SUBMITTED,
                passwordMap = null,
            )
            pendingRepository.save(submitted)
            submitted
        } catch (error: Exception) {
            val unknown = payment.copy(
                stage = PaymentStage.UNKNOWN,
                passwordMap = null,
            )
            pendingRepository.save(unknown)
            throw when (error) {
                is SchoolException -> SchoolException(
                    SchoolErrorKind.PAYMENT_UNKNOWN,
                    "${error.message}。支付结果可能尚未确认，请先核对订单，不要重复提交。",
                    error,
                )
                else -> SchoolException(
                    SchoolErrorKind.PAYMENT_UNKNOWN,
                    "支付结果可能尚未确认，请先核对订单，不要重复提交。",
                    error,
                )
            }
        }
    }

    suspend fun cancel(orderId: String) {
        val payment = pendingRepository.get(orderId)
            ?: throw SchoolException(SchoolErrorKind.BAD_RESPONSE, "待支付订单不存在")
        if (payment.stage == PaymentStage.SUBMITTING ||
            payment.stage == PaymentStage.SUBMITTED ||
            payment.stage == PaymentStage.UNKNOWN
        ) {
            throw SchoolException(SchoolErrorKind.BAD_RESPONSE, "当前订单不能直接取消")
        }
        val client = client()
        val (order, _) = client.getOrder(orderId)
        if (order.status != 0) {
            throw SchoolException(SchoolErrorKind.BAD_RESPONSE, "只能取消待支付订单")
        }
        client.cancelOrder(orderId)
        pendingRepository.delete(orderId)
    }

    private suspend fun client(): SchoolChargeClient {
        val session = sessionStore.load()
            ?: throw SchoolException(SchoolErrorKind.AUTH, "请先登录学校账号")
        if (session.isExpired()) {
            throw SchoolException(SchoolErrorKind.AUTH, "学校登录已过期，请重新登录")
        }
        return chargeClientFactory(session)
    }

    private fun cardMethod(methods: List<PaymentMethod>): PaymentMethod =
        methods.firstOrNull { it.code == "CARDTSM" && it.payId > 0 }
            ?: throw SchoolException(
                SchoolErrorKind.BAD_RESPONSE,
                "当前订单没有校园卡余额支付方式",
            )

    private fun matchesRoom(order: RechargeOrder, target: Target): Boolean {
        val room = order.room ?: return false
        return room.roomId == target.room ||
            room.room == target.room ||
            (room.campus == target.campus &&
                room.building == target.building &&
                room.room == target.room) ||
            order.abstracts.contains(target.room)
    }

    private companion object {
        const val PAYMENT_TTL_MILLIS = 30 * 60 * 1000L
    }
}
