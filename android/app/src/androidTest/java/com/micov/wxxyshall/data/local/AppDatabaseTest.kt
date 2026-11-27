package com.micov.wxxyshall.data.local

import androidx.room.Room
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class AppDatabaseTest {
    private lateinit var database: AppDatabase

    @Before
    fun setUp() {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        database = Room.inMemoryDatabaseBuilder(context, AppDatabase::class.java)
            .allowMainThreadQueries()
            .build()
    }

    @After
    fun tearDown() {
        database.close()
    }

    @Test
    fun storesTargetsAndReturnsLatestReading() = runBlocking {
        val target = TargetEntity(
            key = "A|B|C",
            feeItemId = 409,
            appId = 34,
            campus = "A",
            building = "B",
            room = "C",
            label = "测试宿舍",
            pollIntervalMinutes = 60,
            notifyMode = "alert",
            notifyTime = "08:00",
            lowBalanceThreshold = 10.0,
            nextDueAt = 0,
            createdAt = 1,
        )
        database.targetDao().upsert(target)
        database.readingDao().insert(
            ReadingEntity(
                targetKey = target.key,
                label = target.label,
                campus = target.campus,
                building = target.building,
                room = target.room,
                epoch = 100,
                surplusCharge = 12.3,
                totalUsage = 200.0,
                showJson = """{"电表总用电量":"200"}""",
                rawJson = "{}",
            ),
        )
        database.readingDao().insert(
            ReadingEntity(
                targetKey = target.key,
                label = target.label,
                campus = target.campus,
                building = target.building,
                room = target.room,
                epoch = 200,
                surplusCharge = 11.8,
                totalUsage = 201.0,
                showJson = """{"电表总用电量":"201"}""",
                rawJson = "{}",
            ),
        )

        assertEquals(1, database.targetDao().getAll().size)
        assertEquals(11.8, database.readingDao().latest(target.key)?.surplusCharge ?: 0.0, 0.001)
        assertEquals(1, database.readingDao().observeLatest().first().size)
        assertEquals(1, database.targetDao().due(300).size)
    }
}
