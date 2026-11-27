package com.micov.wxxyshall.data

import com.micov.wxxyshall.data.local.TargetDao
import com.micov.wxxyshall.data.local.toDomain
import com.micov.wxxyshall.data.local.toEntity
import com.micov.wxxyshall.domain.MIN_POLL_INTERVAL_MINUTES
import com.micov.wxxyshall.domain.Target
import com.micov.wxxyshall.domain.TargetDraft
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map

class TargetRepository(private val dao: TargetDao) {
    val targets: Flow<List<Target>> = dao.observeAll().map { rows ->
        rows.map { it.toDomain() }
    }

    suspend fun all(): List<Target> = dao.getAll().map { it.toDomain() }

    suspend fun upsert(target: Target) {
        require(target.pollIntervalMinutes >= MIN_POLL_INTERVAL_MINUTES) {
            "采集周期不能少于 $MIN_POLL_INTERVAL_MINUTES 分钟"
        }
        dao.upsert(target.toEntity())
    }

    suspend fun add(draft: TargetDraft): Target {
        require(draft.campus.isNotBlank() && draft.building.isNotBlank() && draft.room.isNotBlank())
        val key = Target.key(draft.campus, draft.building, draft.room)
        val existing = dao.get(key)
        val target = Target(
            key = key,
            campus = draft.campus.trim(),
            building = draft.building.trim(),
            room = draft.room.trim(),
            label = draft.label.trim().ifBlank {
                "${draft.campus}/${draft.building}/${draft.room}"
            },
            pollIntervalMinutes = draft.pollIntervalMinutes.coerceAtLeast(MIN_POLL_INTERVAL_MINUTES),
            notifyMode = draft.notifyMode,
            notifyTime = draft.notifyTime,
            lowBalanceThreshold = draft.lowBalanceThreshold.coerceAtLeast(0.0),
            nextDueAt = 0,
            createdAt = existing?.createdAt ?: System.currentTimeMillis(),
        )
        dao.upsert(target.toEntity())
        return target
    }

    suspend fun delete(key: String) {
        dao.deleteReadings(key)
        dao.deleteNotificationState(key)
        dao.delete(key)
    }

    suspend fun updateNextDue(key: String, nextDueAt: Long) {
        dao.updateNextDue(key, nextDueAt)
    }

    suspend fun due(now: Long): List<Target> = dao.due(now).map { it.toDomain() }
}
