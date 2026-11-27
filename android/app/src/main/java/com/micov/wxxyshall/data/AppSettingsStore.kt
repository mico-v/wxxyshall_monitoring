package com.micov.wxxyshall.data

import android.content.Context
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

class AppSettingsStore(context: Context) {
    private val prefs = context.getSharedPreferences("app-settings", Context.MODE_PRIVATE)
    private val _biometricPaymentsEnabled = MutableStateFlow(
        prefs.getBoolean(KEY_BIOMETRIC_PAYMENTS, false),
    )
    val biometricPaymentsEnabled: StateFlow<Boolean> =
        _biometricPaymentsEnabled.asStateFlow()

    fun setBiometricPaymentsEnabled(enabled: Boolean) {
        prefs.edit().putBoolean(KEY_BIOMETRIC_PAYMENTS, enabled).apply()
        _biometricPaymentsEnabled.value = enabled
    }

    private companion object {
        const val KEY_BIOMETRIC_PAYMENTS = "biometric_payments_enabled"
    }
}
