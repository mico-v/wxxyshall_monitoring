package com.micov.wxxyshall.ui

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import com.micov.wxxyshall.WxxyshallApp
import com.micov.wxxyshall.domain.Option
import com.micov.wxxyshall.domain.PendingPayment
import com.micov.wxxyshall.domain.Reading
import com.micov.wxxyshall.domain.RechargeOrder
import com.micov.wxxyshall.domain.SchoolException
import com.micov.wxxyshall.domain.SchoolSession
import com.micov.wxxyshall.domain.Target
import com.micov.wxxyshall.domain.TargetDraft
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

class AppViewModel(application: Application) : AndroidViewModel(application) {
    private val container = (application as WxxyshallApp).container

    private val _session = MutableStateFlow<SchoolSession?>(null)
    val session: StateFlow<SchoolSession?> = _session.asStateFlow()
    private val _sessionReady = MutableStateFlow(false)
    val sessionReady: StateFlow<Boolean> = _sessionReady.asStateFlow()
    private val _loginBusy = MutableStateFlow(false)
    val loginBusy: StateFlow<Boolean> = _loginBusy.asStateFlow()
    private val _message = MutableStateFlow<String?>(null)
    val message: StateFlow<String?> = _message.asStateFlow()
    private val _preparedPayment = MutableStateFlow<PendingPayment?>(null)
    val preparedPayment: StateFlow<PendingPayment?> = _preparedPayment.asStateFlow()
    val biometricPaymentsEnabled = container.appSettings.biometricPaymentsEnabled

    val targets: StateFlow<List<Target>> = container.targetRepository.targets.stateIn(
        viewModelScope,
        SharingStarted.WhileSubscribed(5_000),
        emptyList(),
    )
    val latestReadings: StateFlow<Map<String, Reading>> =
        container.readingRepository.observeLatest().stateIn(
            viewModelScope,
            SharingStarted.WhileSubscribed(5_000),
            emptyMap(),
        )
    val pendingPayment: StateFlow<PendingPayment?> =
        container.pendingPaymentRepository.latest.stateIn(
            viewModelScope,
            SharingStarted.WhileSubscribed(5_000),
            null,
        )

    init {
        viewModelScope.launch {
            _session.value = container.sessionStore.load()
            _sessionReady.value = true
            if (_session.value != null) container.scheduler.collectNow()
        }
    }

    fun login(username: String, password: String) {
        if (_loginBusy.value) return
        viewModelScope.launch {
            _loginBusy.value = true
            _message.value = null
            try {
                val next = withContext(Dispatchers.IO) {
                    container.loginClient.login(username, password)
                }
                container.sessionStore.save(next)
                container.clearChargeSessions()
                _session.value = next
                container.scheduler.collectNow()
            } catch (error: SchoolException) {
                _message.value = error.message
            } catch (error: Exception) {
                _message.value = error.message ?: "登录失败"
            } finally {
                _loginBusy.value = false
            }
        }
    }

    fun logout() {
        viewModelScope.launch {
            container.logoutAndClear()
            _preparedPayment.value = null
            _session.value = null
            _message.value = null
        }
    }

    fun collectNow() {
        container.scheduler.collectNow()
        _message.value = "已开始采集"
    }

    fun clearMessage() {
        _message.value = null
    }

    fun readings(targetKey: String): Flow<List<Reading>> =
        container.readingRepository.observe(targetKey)

    suspend fun loadCampuses(): Result<List<Option>> = schoolCall {
        container.chargeClient(requireSession()).listCampuses()
    }

    suspend fun loadBuildings(campus: String): Result<List<Option>> = schoolCall {
        container.chargeClient(requireSession()).listBuildings(campus)
    }

    suspend fun loadRooms(campus: String, building: String): Result<List<Option>> = schoolCall {
        container.chargeClient(requireSession()).listRooms(campus, building)
    }

    suspend fun saveTarget(existing: Target?, draft: TargetDraft): Result<Target> = runCatching {
        validateDraft(draft)
        val saved = container.targetRepository.add(draft)
        if (existing != null && existing.key != saved.key) {
            container.targetRepository.delete(existing.key)
        }
        container.scheduler.collectNow()
        saved
    }

    suspend fun deleteTarget(target: Target): Result<Unit> = runCatching {
        container.targetRepository.delete(target.key)
    }

    suspend fun loadOrders(status: Int): Result<List<RechargeOrder>> = schoolCall {
        container.rechargeRepository.listOrders(status)
    }

    suspend fun createRecharge(target: Target, amount: Double): Result<PendingPayment> = schoolCall {
        container.rechargeRepository.createOrder(target, amount)
    }

    suspend fun preparePayment(orderId: String): Result<PendingPayment> = schoolCall {
        container.rechargeRepository.prepare(orderId).also {
            _preparedPayment.value = it
        }
    }

    suspend fun submitPayment(indexes: String): Result<PendingPayment> {
        val payment = _preparedPayment.value
            ?: return Result.failure(SchoolException(
                com.micov.wxxyshall.domain.SchoolErrorKind.BAD_RESPONSE,
                "请先获取支付安全键盘",
            ))
        return schoolCall {
            container.rechargeRepository.submit(payment, indexes).also {
                _preparedPayment.value = null
            }
        }
    }

    suspend fun cancelPayment(orderId: String): Result<Unit> = schoolCall {
        container.rechargeRepository.cancel(orderId)
        _preparedPayment.value = null
    }

    fun clearPreparedPayment() {
        _preparedPayment.value = null
    }

    fun setBiometricPaymentsEnabled(enabled: Boolean) {
        container.appSettings.setBiometricPaymentsEnabled(enabled)
    }

    private fun requireSession(): SchoolSession =
        _session.value ?: throw SchoolException(
            com.micov.wxxyshall.domain.SchoolErrorKind.AUTH,
            "请先登录学校账号",
        )

    private suspend fun <T> schoolCall(block: suspend () -> T): Result<T> =
        withContext(Dispatchers.IO) {
            try {
                Result.success(block())
            } catch (error: SchoolException) {
                if (error.kind == com.micov.wxxyshall.domain.SchoolErrorKind.AUTH) {
                    container.sessionStore.clear()
                    container.clearChargeSessions()
                    _session.value = null
                }
                Result.failure(error)
            } catch (error: Exception) {
                Result.failure(error)
            }
        }

    private fun validateDraft(draft: TargetDraft) {
        require(draft.campus.isNotBlank() && draft.building.isNotBlank() && draft.room.isNotBlank()) {
            "请选择校区、楼栋和房间"
        }
        require(draft.pollIntervalMinutes >= 15 && draft.pollIntervalMinutes <= 10_080) {
            "采集周期必须在 15..10080 分钟之间"
        }
        require(draft.lowBalanceThreshold.isFinite() && draft.lowBalanceThreshold >= 0) {
            "低余额阈值必须是非负数"
        }
        require(Regex("\\d{2}:\\d{2}").matches(draft.notifyTime)) {
            "每日通知时间必须是 HH:MM"
        }
    }
}
