package com.micov.wxxyshall.data.local

import androidx.room.Dao
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.Query
import androidx.room.Upsert
import kotlinx.coroutines.flow.Flow

@Dao
interface TargetDao {
    @Query("SELECT * FROM targets ORDER BY createdAt ASC")
    fun observeAll(): Flow<List<TargetEntity>>

    @Query("SELECT * FROM targets ORDER BY createdAt ASC")
    suspend fun getAll(): List<TargetEntity>

    @Query("SELECT * FROM targets WHERE key = :key")
    suspend fun get(key: String): TargetEntity?

    @Query("SELECT * FROM targets WHERE nextDueAt <= :now ORDER BY createdAt ASC")
    suspend fun due(now: Long): List<TargetEntity>

    @Upsert
    suspend fun upsert(target: TargetEntity)

    @Query("UPDATE targets SET nextDueAt = :nextDueAt WHERE key = :key")
    suspend fun updateNextDue(key: String, nextDueAt: Long)

    @Query("DELETE FROM targets WHERE key = :key")
    suspend fun delete(key: String)

    @Query("DELETE FROM readings WHERE targetKey = :key")
    suspend fun deleteReadings(key: String)

    @Query("DELETE FROM notification_state WHERE targetKey = :key")
    suspend fun deleteNotificationState(key: String)
}

@Dao
interface ReadingDao {
    @Insert
    suspend fun insert(reading: ReadingEntity): Long

    @Query("SELECT * FROM readings WHERE targetKey = :key ORDER BY epoch ASC, id ASC LIMIT 10000")
    fun observeByTarget(key: String): Flow<List<ReadingEntity>>

    @Query("SELECT * FROM readings WHERE targetKey = :key ORDER BY epoch DESC, id DESC LIMIT 1")
    suspend fun latest(key: String): ReadingEntity?

    @Query(
        """
        SELECT r.* FROM readings r
        INNER JOIN (
            SELECT targetKey, MAX(epoch) AS maxEpoch
            FROM readings
            GROUP BY targetKey
        ) latest ON latest.targetKey = r.targetKey AND latest.maxEpoch = r.epoch
        ORDER BY r.id DESC
        """,
    )
    fun observeLatest(): Flow<List<ReadingEntity>>
}

@Dao
interface NotificationStateDao {
    @Query("SELECT * FROM notification_state WHERE targetKey = :key")
    suspend fun get(key: String): NotificationStateEntity?

    @Upsert
    suspend fun upsert(state: NotificationStateEntity)
}

@Dao
interface PendingPaymentDao {
    @Upsert
    suspend fun upsert(payment: PendingPaymentEntity)

    @Query("SELECT * FROM pending_payments WHERE orderId = :orderId")
    suspend fun get(orderId: String): PendingPaymentEntity?

    @Query("SELECT * FROM pending_payments ORDER BY createdAt DESC LIMIT 1")
    fun observeLatest(): Flow<PendingPaymentEntity?>

    @Query("DELETE FROM pending_payments WHERE orderId = :orderId")
    suspend fun delete(orderId: String)

    @Query("DELETE FROM pending_payments WHERE expiresAt < :now")
    suspend fun deleteExpired(now: Long)
}
