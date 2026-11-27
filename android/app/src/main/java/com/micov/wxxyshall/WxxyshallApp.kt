package com.micov.wxxyshall

import android.app.Application

class WxxyshallApp : Application() {
    val container: AppContainer by lazy { AppContainer(this) }

    override fun onCreate() {
        super.onCreate()
        container.createNotificationChannels()
        container.scheduler.schedule()
    }
}
