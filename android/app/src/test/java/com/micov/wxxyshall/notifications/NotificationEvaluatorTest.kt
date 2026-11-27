package com.micov.wxxyshall.notifications

import com.micov.wxxyshall.data.local.NotificationStateEntity
import com.micov.wxxyshall.domain.NotificationMode
import com.micov.wxxyshall.domain.Reading
import com.micov.wxxyshall.domain.Target
import java.time.LocalDateTime
import java.time.ZoneId
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class NotificationEvaluatorTest {
    private val zone = ZoneId.of("Asia/Shanghai")
    private val evaluator = NotificationEvaluator(zone)
    private val target = Target(
        key = "A|B|C",
        campus = "A",
        building = "B",
        room = "C",
        label = "C",
        notifyMode = NotificationMode.ALERT,
        lowBalanceThreshold = 10.0,
    )

    private fun reading(value: Double) = Reading(
        surplusCharge = value,
        show = emptyMap(),
        raw = emptyMap(),
    )

    private fun millis(hour: Int, minute: Int): Long =
        LocalDateTime.of(2026, 11, 27, hour, minute)
            .atZone(zone)
            .toInstant()
            .toEpochMilli()

    @Test
    fun lowBalanceNotifiesOnlyWhenEnteringRange() {
        val entered = evaluator.evaluate(
            target,
            reading(8.0),
            previous = reading(12.0),
            state = NotificationStateEntity(target.key),
        )
        val continued = evaluator.evaluate(
            target,
            reading(7.0),
            previous = reading(8.0),
            state = NotificationStateEntity(target.key, wasLow = true),
        )

        assertEquals(NotificationKind.LOW_BALANCE, entered?.kind)
        assertNull(continued)
    }

    @Test
    fun lowBalanceCanNotifyAgainAfterRecovery() {
        evaluator.evaluate(
            target,
            reading(8.0),
            previous = reading(12.0),
            state = NotificationStateEntity(target.key),
        )
        val recovered = evaluator.evaluate(
            target,
            reading(12.0),
            previous = reading(8.0),
            state = NotificationStateEntity(target.key, wasLow = true),
        )
        val droppedAgain = evaluator.evaluate(
            target,
            reading(9.0),
            previous = reading(12.0),
            state = NotificationStateEntity(target.key, wasLow = false),
        )

        assertNull(recovered)
        assertEquals(NotificationKind.LOW_BALANCE, droppedAgain?.kind)
    }

    @Test
    fun dailyNotificationIsSentOnceAfterConfiguredTime() {
        val daily = target.copy(
            notifyMode = NotificationMode.DAILY,
            notifyTime = "08:00",
        )
        val early = evaluator.evaluate(
            daily,
            reading(20.0),
            previous = null,
            state = NotificationStateEntity(target.key),
            nowMillis = millis(7, 59),
        )
        val due = evaluator.evaluate(
            daily,
            reading(20.0),
            previous = null,
            state = NotificationStateEntity(target.key),
            nowMillis = millis(8, 1),
        )
        val duplicate = evaluator.evaluate(
            daily,
            reading(19.0),
            previous = null,
            state = due!!.nextState,
            nowMillis = millis(9, 0),
        )

        assertNull(early)
        assertEquals(NotificationKind.DAILY, due.kind)
        assertNull(duplicate)
    }
}
