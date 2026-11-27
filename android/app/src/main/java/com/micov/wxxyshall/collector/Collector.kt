package com.micov.wxxyshall.collector

import com.micov.wxxyshall.data.AuthAlertStore
import com.micov.wxxyshall.data.ReadingRepository
import com.micov.wxxyshall.data.TargetRepository
import com.micov.wxxyshall.data.local.NotificationStateDao
import com.micov.wxxyshall.data.local.NotificationStateEntity
import com.micov.wxxyshall.data.remote.SchoolChargeClient
import com.micov.wxxyshall.domain.SchoolErrorKind
import com.micov.wxxyshall.domain.SchoolException
import com.micov.wxxyshall.domain.SchoolSession
import com.micov.wxxyshall.notifications.NotificationCoordinator
import com.micov.wxxyshall.notifications.NotificationEvaluator
import com.micov.wxxyshall.security.SecureSessionStore
import kotlinx.coroutines.sync.Mutex

data class CollectionResult(
    val success: Int,
    val failed: Int,
    val errors: List<String>,
    val authExpired: Boolean = false,
    val busy: Boolean = false,
)

class Collector(
    private val targetRepository: TargetRepository,
    private val readingRepository: ReadingRepository,
    private val notificationDao: NotificationStateDao,
    private val sessionStore: SecureSessionStore,
    private val chargeClientFactory: (SchoolSession) -> SchoolChargeClient,
    private val notifications: NotificationCoordinator,
    private val authAlertStore: AuthAlertStore,
    private val evaluator: NotificationEvaluator = NotificationEvaluator(),
) {
    private val mutex = Mutex()

    suspend fun collectDue(now: Long = System.currentTimeMillis()): CollectionResult {
        if (!mutex.tryLock()) {
            return CollectionResult(0, 0, listOf("已有采集任务正在运行"), busy = true)
        }
        try {
            val targets = targetRepository.due(now)
            if (targets.isEmpty()) return CollectionResult(0, 0, emptyList())
            return collect(targets, now)
        } finally {
            mutex.unlock()
        }
    }

    suspend fun collectAll(now: Long = System.currentTimeMillis()): CollectionResult {
        if (!mutex.tryLock()) {
            return CollectionResult(0, 0, listOf("已有采集任务正在运行"), busy = true)
        }
        try {
            return collect(targetRepository.all(), now)
        } finally {
            mutex.unlock()
        }
    }

    private suspend fun collect(
        targets: List<com.micov.wxxyshall.domain.Target>,
        now: Long,
    ): CollectionResult {
        if (targets.isEmpty()) return CollectionResult(0, 0, emptyList())
        val session = sessionStore.load()
        if (session == null || session.isExpired()) {
            notifyExpiredOnce()
            return CollectionResult(
                success = 0,
                failed = targets.size,
                errors = listOf("学校登录已失效，请重新登录"),
                authExpired = true,
            )
        }
        val charge = chargeClientFactory(session)
        var success = 0
        var failed = 0
        val errors = mutableListOf<String>()
        var authExpired = false
        for (target in targets) {
            try {
                val snapshot = charge.queryRoomSnapshot(
                    target.campus,
                    target.building,
                    target.room,
                    target.feeItemId,
                )
                val previous = readingRepository.latest(target.key)
                readingRepository.insert(target, snapshot.reading)
                evaluator.evaluate(
                    target = target,
                    reading = snapshot.reading,
                    previous = previous,
                    state = notificationDao.get(target.key) ?: NotificationStateEntity(target.key),
                )?.let { decision ->
                    if (notifications.notifyReading(target, snapshot.reading, decision.kind)) {
                        notificationDao.upsert(decision.nextState)
                    }
                }
                success++
            } catch (error: SchoolException) {
                failed++
                errors += "${target.label}: ${error.message}"
                if (error.kind == SchoolErrorKind.AUTH) {
                    authExpired = true
                    notifyExpiredOnce()
                    break
                }
            } catch (error: Exception) {
                failed++
                errors += "${target.label}: ${error.message.orEmpty()}"
            } finally {
                targetRepository.updateNextDue(
                    target.key,
                    now + target.pollIntervalMinutes * 60_000L,
                )
            }
        }
        if (!authExpired) authAlertStore.reset()
        return CollectionResult(success, failed, errors, authExpired)
    }

    private fun notifyExpiredOnce() {
        if (authAlertStore.shouldNotifyExpired()) {
            notifications.notifyAuthExpired()
            authAlertStore.markExpiredNotified()
        }
    }
}
