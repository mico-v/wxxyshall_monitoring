package com.micov.wxxyshall.data.local

import androidx.room.Entity
import androidx.room.Index
import androidx.room.PrimaryKey

@Entity(tableName = "targets")
data class TargetEntity(
    @PrimaryKey val key: String,
    val feeItemId: Int,
    val appId: Int,
    val campus: String,
    val building: String,
    val room: String,
    val label: String,
    val pollIntervalMinutes: Int,
    val notifyMode: String,
    val notifyTime: String,
    val lowBalanceThreshold: Double,
    val nextDueAt: Long,
    val createdAt: Long,
)

@Entity(
    tableName = "readings",
    indices = [
        Index(value = ["targetKey", "epoch"]),
        Index(value = ["epoch"]),
    ],
)
data class ReadingEntity(
    @PrimaryKey(autoGenerate = true) val id: Long = 0,
    val targetKey: String,
    val label: String,
    val campus: String,
    val building: String,
    val room: String,
    val epoch: Long,
    val surplusCharge: Double?,
    val totalUsage: Double?,
    val showJson: String,
    val rawJson: String,
)

@Entity(tableName = "notification_state")
data class NotificationStateEntity(
    @PrimaryKey val targetKey: String,
    val wasLow: Boolean = false,
    val lastDailyDay: String = "",
)

@Entity(tableName = "pending_payments")
data class PendingPaymentEntity(
    @PrimaryKey val orderId: String,
    val targetKey: String,
    val roomLabel: String,
    val amount: Double,
    val payId: Int,
    val methodName: String,
    val methodCode: String,
    val methodNoPassword: Int,
    val methodChooseAccount: Boolean,
    val stage: String,
    val expiresAt: Long,
    val createdAt: Long,
)
