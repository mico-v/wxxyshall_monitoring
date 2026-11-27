package com.micov.wxxyshall.data.local

import androidx.room.Database
import androidx.room.RoomDatabase

@Database(
    entities = [
        TargetEntity::class,
        ReadingEntity::class,
        NotificationStateEntity::class,
        PendingPaymentEntity::class,
    ],
    version = 1,
    exportSchema = true,
)
abstract class AppDatabase : RoomDatabase() {
    abstract fun targetDao(): TargetDao
    abstract fun readingDao(): ReadingDao
    abstract fun notificationStateDao(): NotificationStateDao
    abstract fun pendingPaymentDao(): PendingPaymentDao
}
