package com.micov.wxxyshall.data

import android.content.Context

class AuthAlertStore(context: Context) {
    private val prefs = context.getSharedPreferences("auth-alert", Context.MODE_PRIVATE)

    fun shouldNotifyExpired(): Boolean = !prefs.getBoolean(KEY_EXPIRED_NOTIFIED, false)

    fun markExpiredNotified() {
        prefs.edit().putBoolean(KEY_EXPIRED_NOTIFIED, true).apply()
    }

    fun reset() {
        prefs.edit().putBoolean(KEY_EXPIRED_NOTIFIED, false).apply()
    }

    private companion object {
        const val KEY_EXPIRED_NOTIFIED = "expired_notified"
    }
}
