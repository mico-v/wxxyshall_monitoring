package com.micov.wxxyshall.notifications

import com.micov.wxxyshall.data.local.NotificationStateEntity
import com.micov.wxxyshall.domain.Reading
import com.micov.wxxyshall.domain.Target
import java.time.Instant
import java.time.LocalDate
import java.time.LocalTime
import java.time.ZoneId

import com.micov.wxxyshall.domain.NotificationMode

enum class NotificationKind {
    LOW_BALANCE,
    DAILY,
}

data class NotificationDecision(
    val kind: NotificationKind,
    val nextState: NotificationStateEntity,
)

class NotificationEvaluator(
    private val zoneId: ZoneId = ZoneId.systemDefault(),
) {
    fun evaluate(
        target: Target,
        reading: Reading,
        previous: Reading?,
        state: NotificationStateEntity,
        nowMillis: Long = System.currentTimeMillis(),
    ): NotificationDecision? {
        val value = reading.surplusCharge ?: return null
        val date = Instant.ofEpochMilli(nowMillis).atZone(zoneId).toLocalDate()
        val nextState = state
        val previousValue = previous?.surplusCharge

        if (target.notifyMode == NotificationMode.ALERT) {
            val isLow = value <= target.lowBalanceThreshold
            val enteredLow = isLow && (previousValue == null || previousValue > target.lowBalanceThreshold)
            if (enteredLow) {
                return NotificationDecision(
                    NotificationKind.LOW_BALANCE,
                    nextState.copy(wasLow = isLow),
                )
            }
        }

        if (target.notifyMode == NotificationMode.DAILY) {
            val dueTime = runCatching { LocalTime.parse(target.notifyTime) }
                .getOrDefault(LocalTime.of(8, 0))
            val localTime = Instant.ofEpochMilli(nowMillis).atZone(zoneId).toLocalTime()
            if (!localTime.isBefore(dueTime) && nextState.lastDailyDay != date.toString()) {
                return NotificationDecision(
                    NotificationKind.DAILY,
                    nextState.copy(lastDailyDay = date.toString()),
                )
            }
        }
        return null
    }

    fun today(nowMillis: Long = System.currentTimeMillis()): LocalDate =
        Instant.ofEpochMilli(nowMillis).atZone(zoneId).toLocalDate()
}
