package com.micov.wxxyshall.data

import com.micov.wxxyshall.data.local.ReadingDao
import com.micov.wxxyshall.data.local.ReadingEntity
import com.micov.wxxyshall.data.local.toDomain
import com.micov.wxxyshall.domain.Reading
import com.micov.wxxyshall.domain.Target
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json

class ReadingRepository(
    private val dao: ReadingDao,
    private val json: Json = Json,
) {
    fun observe(targetKey: String): Flow<List<Reading>> =
        dao.observeByTarget(targetKey).map { rows -> rows.map { it.toDomain() } }

    fun observeLatest(): Flow<Map<String, Reading>> =
        dao.observeLatest().map { rows ->
            rows.associate { it.targetKey to it.toDomain() }
        }

    suspend fun latest(targetKey: String): Reading? = dao.latest(targetKey)?.toDomain()

    suspend fun insert(target: Target, reading: Reading): Reading {
        dao.insert(
            ReadingEntity(
                targetKey = target.key,
                label = target.label,
                campus = target.campus,
                building = target.building,
                room = target.room,
                epoch = reading.epoch,
                surplusCharge = reading.surplusCharge,
                totalUsage = reading.totalUsage,
                showJson = json.encodeToString(reading.show),
                rawJson = json.encodeToString(reading.raw),
            ),
        )
        return reading
    }
}
