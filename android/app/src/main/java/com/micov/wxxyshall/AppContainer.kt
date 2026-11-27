package com.micov.wxxyshall

import android.content.Context
import androidx.room.Room
import com.micov.wxxyshall.collector.Collector
import com.micov.wxxyshall.data.AuthAlertStore
import com.micov.wxxyshall.data.AppSettingsStore
import com.micov.wxxyshall.data.PendingPaymentRepository
import com.micov.wxxyshall.data.ReadingRepository
import com.micov.wxxyshall.data.RechargeRepository
import com.micov.wxxyshall.data.TargetRepository
import com.micov.wxxyshall.data.local.AppDatabase
import com.micov.wxxyshall.data.remote.RequestPacer
import com.micov.wxxyshall.data.remote.SchoolChargeClient
import com.micov.wxxyshall.data.remote.SchoolLoginClient
import com.micov.wxxyshall.domain.DEFAULT_BASE_URL
import com.micov.wxxyshall.domain.SchoolSession
import com.micov.wxxyshall.notifications.NotificationCoordinator
import com.micov.wxxyshall.security.SecureSessionStore
import com.micov.wxxyshall.work.CollectionScheduler
import kotlinx.serialization.json.Json

class AppContainer(context: Context) {
    private val appContext = context.applicationContext
    val json = Json { ignoreUnknownKeys = true }
    val database: AppDatabase = Room.databaseBuilder(
        appContext,
        AppDatabase::class.java,
        "wxxyshall.db",
    ).build()
    val sessionStore = SecureSessionStore(appContext)
    val appSettings = AppSettingsStore(appContext)
    val targetRepository = TargetRepository(database.targetDao())
    val readingRepository = ReadingRepository(database.readingDao(), json)
    val pendingPaymentRepository = PendingPaymentRepository(database.pendingPaymentDao())
    private val pacer = RequestPacer()
    private val notifications = NotificationCoordinator(appContext)
    private val authAlertStore = AuthAlertStore(appContext)
    val loginClient = SchoolLoginClient(DEFAULT_BASE_URL)
    val scheduler = CollectionScheduler(appContext)
    private val chargeClients = mutableMapOf<String, SchoolChargeClient>()
    val rechargeRepository = RechargeRepository(
        sessionStore = sessionStore,
        pendingRepository = pendingPaymentRepository,
        chargeClientFactory = { session -> chargeClient(session) },
    )

    val collector = Collector(
        targetRepository = targetRepository,
        readingRepository = readingRepository,
        notificationDao = database.notificationStateDao(),
        sessionStore = sessionStore,
        chargeClientFactory = { session -> chargeClient(session) },
        notifications = notifications,
        authAlertStore = authAlertStore,
    )

    fun chargeClient(session: SchoolSession): SchoolChargeClient = synchronized(chargeClients) {
        chargeClients.getOrPut(session.accessToken) {
            SchoolChargeClient(
                baseUrl = DEFAULT_BASE_URL,
                token = session.accessToken,
                pacer = pacer,
                json = json,
            )
        }
    }

    fun forgetChargeSession(session: SchoolSession) {
        synchronized(chargeClients) {
            chargeClients.remove(session.accessToken)
        }
    }

    fun clearChargeSessions() {
        synchronized(chargeClients) { chargeClients.clear() }
    }

    fun createNotificationChannels() = notifications.createChannels()

    suspend fun logoutAndClear() {
        val current = sessionStore.load()
        sessionStore.clear()
        if (current != null) forgetChargeSession(current)
    }
}
