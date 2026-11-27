package com.micov.wxxyshall.ui.screens

import android.app.Activity
import android.view.WindowManager
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Backspace
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.material.icons.outlined.Payments
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.micov.wxxyshall.domain.MAX_RECHARGE_AMOUNT
import com.micov.wxxyshall.domain.MIN_RECHARGE_AMOUNT
import com.micov.wxxyshall.domain.PaymentStage
import com.micov.wxxyshall.domain.PendingPayment
import com.micov.wxxyshall.domain.Target
import com.micov.wxxyshall.ui.AppViewModel
import com.micov.wxxyshall.ui.BiometricAuth
import com.micov.wxxyshall.ui.components.BalancePill
import com.micov.wxxyshall.ui.components.EmptyBlock
import com.micov.wxxyshall.ui.components.PageTopBar
import com.micov.wxxyshall.ui.components.formatBalance
import com.micov.wxxyshall.ui.components.orderTail
import java.util.Locale
import kotlinx.coroutines.launch

@Composable
fun RechargeScreen(
    viewModel: AppViewModel,
    target: Target?,
    biometric: BiometricAuth,
    onBack: () -> Unit,
) {
    if (target == null) {
        Scaffold(topBar = { PageTopBar("校园卡充值", onBack) }) { padding ->
            EmptyBlock("找不到宿舍", Modifier.padding(padding))
        }
        return
    }
    val context = LocalContext.current
    DisposableEffect(Unit) {
        val window = (context as? Activity)?.window
        window?.addFlags(WindowManager.LayoutParams.FLAG_SECURE)
        onDispose { window?.clearFlags(WindowManager.LayoutParams.FLAG_SECURE) }
    }

    val latestMap by viewModel.latestReadings.collectAsStateWithLifecycle()
    val pending by viewModel.pendingPayment.collectAsStateWithLifecycle()
    val prepared by viewModel.preparedPayment.collectAsStateWithLifecycle()
    val biometricEnabled by viewModel.biometricPaymentsEnabled.collectAsStateWithLifecycle()
    val activePending = pending?.takeIf {
        it.targetKey == target.key && it.expiresAt > System.currentTimeMillis()
    }
    var amountText by remember { mutableStateOf("10") }
    var authorized by remember { mutableStateOf(false) }
    var authError by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf("") }
    var indexes by remember { mutableStateOf("") }
    val scope = rememberCoroutineScope()

    fun requestAuth(onSuccess: () -> Unit) {
        if (!biometric.canAuthenticate()) {
            authError = "当前设备未配置可用的生物识别或锁屏凭据"
            return
        }
        biometric.authenticate(
            title = "确认校园卡充值",
            subtitle = "验证后继续操作",
            onSuccess = {
                authError = ""
                authorized = true
                onSuccess()
            },
            onError = { authError = it },
        )
    }

    LaunchedEffect(target.key, biometricEnabled) {
        if (!biometricEnabled) {
            authorized = true
            authError = ""
        } else if (!authorized) {
            requestAuth {}
        }
    }
    LaunchedEffect(prepared?.orderId) {
        indexes = ""
    }

    Scaffold(
        topBar = { PageTopBar("校园卡充值", onBack) },
    ) { padding ->
        LazyColumn(
            modifier = Modifier.fillMaxSize().padding(padding).padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            item {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween,
                ) {
                    Column {
                        Text(target.label, style = MaterialTheme.typography.titleLarge)
                        Text(
                            "${formatBalance(latestMap[target.key]?.surplusCharge)} kWh",
                            style = MaterialTheme.typography.headlineSmall,
                        )
                    }
                    BalancePill(
                        latestMap[target.key]?.surplusCharge,
                        target.lowBalanceThreshold,
                    )
                }
            }
            if (!authorized) {
                item {
                    Card(
                        colors = CardDefaults.cardColors(
                            containerColor = MaterialTheme.colorScheme.tertiaryContainer,
                        ),
                    ) {
                        Column(Modifier.padding(16.dp)) {
                            Icon(Icons.Outlined.Lock, contentDescription = null)
                            Text("需要本地身份验证")
                            if (authError.isNotBlank()) {
                                Text(authError, color = MaterialTheme.colorScheme.error)
                            }
                            Button(
                                onClick = { requestAuth {} },
                                modifier = Modifier.padding(top = 12.dp),
                            ) {
                                Text("验证")
                            }
                        }
                    }
                }
            } else if (activePending == null) {
                item {
                    AmountEntry(
                        amountText = amountText,
                        onAmountChange = { amountText = it },
                        busy = busy,
                        onCreate = {
                            val amount = amountText.toDoubleOrNull()
                            if (amount == null) {
                                error = "请输入有效金额"
                            } else {
                                scope.launch {
                                    busy = true
                                    error = ""
                                    viewModel.createRecharge(target, amount)
                                        .onFailure { error = it.message ?: "创建订单失败" }
                                    busy = false
                                }
                            }
                        },
                    )
                }
            }

            activePending?.let { payment ->
                item {
                    PendingOrderCard(payment)
                }
                if (prepared == null &&
                    payment.stage != PaymentStage.SUBMITTING &&
                    payment.stage != PaymentStage.SUBMITTED &&
                    payment.stage != PaymentStage.UNKNOWN
                ) {
                    item {
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            Button(
                                enabled = !busy,
                                onClick = {
                                    scope.launch {
                                        busy = true
                                        error = ""
                                        viewModel.preparePayment(payment.orderId)
                                            .onFailure {
                                                error = it.message ?: "获取支付键盘失败"
                                            }
                                        busy = false
                                    }
                                },
                            ) {
                                Text("继续支付")
                            }
                            OutlinedButton(
                                enabled = !busy,
                                onClick = {
                                    scope.launch {
                                        busy = true
                                        viewModel.cancelPayment(payment.orderId)
                                            .onFailure {
                                                error = it.message ?: "取消订单失败"
                                            }
                                        busy = false
                                    }
                                },
                            ) {
                                Text("取消订单")
                            }
                        }
                    }
                }
                if (payment.stage == PaymentStage.SUBMITTED) {
                    item {
                        StatusCard(
                            "支付请求已提交",
                            "学校订单和电表到账可能延迟，请稍后刷新订单和余额。",
                        )
                    }
                }
                if (payment.stage == PaymentStage.UNKNOWN) {
                    item {
                        StatusCard(
                            "支付结果待确认",
                            "不要重复提交。请先查询订单记录和宿舍余额。",
                            error = true,
                        )
                    }
                }
            }

            prepared?.let { payment ->
                item {
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.SpaceBetween,
                    ) {
                        Text("六位校园卡密码", style = MaterialTheme.typography.titleMedium)
                        IconButton(
                            onClick = {
                                if (indexes.isNotEmpty()) indexes = indexes.dropLast(1)
                            },
                            enabled = indexes.isNotEmpty() && !busy,
                        ) {
                            Icon(Icons.AutoMirrored.Outlined.Backspace, contentDescription = "退格")
                        }
                    }
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        repeat(6) { index ->
                            Text(
                                if (index < indexes.length) "●" else "○",
                                style = MaterialTheme.typography.headlineSmall,
                            )
                        }
                    }
                    val keys = payment.passwordMap?.keys.orEmpty()
                    keys.chunked(5).forEach { row ->
                        Row(
                            modifier = Modifier.fillMaxWidth(),
                            horizontalArrangement = Arrangement.spacedBy(8.dp),
                        ) {
                            row.forEach { digit ->
                                val position = keys.indexOf(digit)
                                OutlinedButton(
                                    onClick = {
                                        if (indexes.length < 6) {
                                            indexes += position.toString()
                                        }
                                    },
                                    enabled = indexes.length < 6 && !busy,
                                    modifier = Modifier.weight(1f),
                                ) {
                                    Text(digit.toString())
                                }
                            }
                            repeat(5 - row.size) {
                                Column(Modifier.weight(1f)) {}
                            }
                        }
                    }
                    Button(
                        enabled = indexes.length == 6 && !busy,
                        onClick = {
                            val submit: () -> Unit = {
                                scope.launch {
                                    busy = true
                                    error = ""
                                    viewModel.submitPayment(indexes)
                                        .onSuccess { indexes = "" }
                                        .onFailure { error = it.message ?: "支付提交失败" }
                                    busy = false
                                }
                                Unit
                            }
                            if (biometricEnabled) requestAuth(submit) else submit()
                        },
                        modifier = Modifier.fillMaxWidth(),
                    ) {
                        Icon(Icons.Outlined.CheckCircle, contentDescription = null)
                        Text(
                            "确认支付 ¥${String.format(Locale.US, "%.2f", payment.amount)}",
                            modifier = Modifier.padding(start = 8.dp),
                        )
                    }
                    OutlinedButton(
                        enabled = !busy,
                        onClick = {
                            indexes = ""
                            viewModel.clearPreparedPayment()
                        },
                        modifier = Modifier.fillMaxWidth(),
                    ) {
                        Text("返回待支付订单")
                    }
                }
            }
            if (error.isNotBlank()) {
                item { Text(error, color = MaterialTheme.colorScheme.error) }
            }
            if (authError.isNotBlank() && authorized) {
                item { Text(authError, color = MaterialTheme.colorScheme.error) }
            }
            item {
                Text(
                    "最终扣款请求不会自动重试。提交后如遇网络异常，请先核对订单和余额。",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

@Composable
private fun AmountEntry(
    amountText: String,
    onAmountChange: (String) -> Unit,
    busy: Boolean,
    onCreate: () -> Unit,
) {
    val amount = amountText.toDoubleOrNull()
    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Text("充值金额", style = MaterialTheme.typography.titleMedium)
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            listOf(10, 50, 100).forEach { value ->
                FilterChip(
                    selected = amount == value.toDouble(),
                    onClick = { onAmountChange(value.toString()) },
                    label = { Text("¥$value") },
                )
            }
        }
        OutlinedTextField(
            value = amountText,
            onValueChange = { onAmountChange(it.replace(',', '.')) },
            label = { Text("自定义金额") },
            prefix = { Text("¥") },
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
            singleLine = true,
            supportingText = {
                Text("单笔 ${MIN_RECHARGE_AMOUNT.toInt()} - ${MAX_RECHARGE_AMOUNT.toInt()} 元")
            },
            modifier = Modifier.fillMaxWidth(),
        )
        Button(
            onClick = onCreate,
            enabled = !busy && amount != null && amount in MIN_RECHARGE_AMOUNT..MAX_RECHARGE_AMOUNT,
            modifier = Modifier.fillMaxWidth(),
        ) {
            Icon(Icons.Outlined.Payments, contentDescription = null)
            Text(
                if (busy) "正在创建订单…" else "创建待支付订单",
                modifier = Modifier.padding(start = 8.dp),
            )
        }
    }
}

@Composable
private fun PendingOrderCard(payment: PendingPayment) {
    Card(
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surface),
    ) {
        Column(Modifier.fillMaxWidth().padding(16.dp)) {
            Text("待支付订单", style = MaterialTheme.typography.titleMedium)
            Text(
                "¥${String.format(Locale.US, "%.2f", payment.amount)}",
                style = MaterialTheme.typography.headlineSmall,
            )
            Text(
                "${payment.method.name} / ${payment.method.code}",
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Text(
                orderTail(payment.orderId),
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                style = MaterialTheme.typography.bodySmall,
            )
        }
    }
}

@Composable
private fun StatusCard(title: String, detail: String, error: Boolean = false) {
    Card(
        colors = CardDefaults.cardColors(
            containerColor = if (error) {
                MaterialTheme.colorScheme.errorContainer
            } else {
                MaterialTheme.colorScheme.primaryContainer
            },
        ),
    ) {
        Column(Modifier.fillMaxWidth().padding(16.dp)) {
            Text(title, style = MaterialTheme.typography.titleMedium)
            Text(detail, style = MaterialTheme.typography.bodySmall)
        }
    }
}
