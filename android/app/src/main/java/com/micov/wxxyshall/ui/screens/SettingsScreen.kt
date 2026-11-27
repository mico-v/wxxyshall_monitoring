package com.micov.wxxyshall.ui.screens

import android.content.Intent
import android.net.Uri
import android.provider.Settings
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.Notifications
import androidx.compose.material.icons.outlined.Fingerprint
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.micov.wxxyshall.domain.Target
import com.micov.wxxyshall.ui.AppViewModel
import com.micov.wxxyshall.ui.components.PageTopBar
import kotlinx.coroutines.launch

@Composable
fun SettingsScreen(
    viewModel: AppViewModel,
    onEditTarget: (Target) -> Unit,
    onAddTarget: () -> Unit,
) {
    val session by viewModel.session.collectAsStateWithLifecycle()
    val targets by viewModel.targets.collectAsStateWithLifecycle()
    val biometricEnabled by viewModel.biometricPaymentsEnabled.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var pendingDelete by remember { mutableStateOf<Target?>(null) }
    var error by remember { mutableStateOf("") }

    Scaffold(
        topBar = { PageTopBar("设置") },
    ) { padding ->
        LazyColumn(
            modifier = Modifier.fillMaxSize().padding(padding),
            contentPadding = PaddingValues(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            item {
                Card(
                    colors = CardDefaults.cardColors(
                        containerColor = MaterialTheme.colorScheme.surface,
                    ),
                ) {
                    Row(
                        modifier = Modifier.fillMaxWidth().padding(16.dp),
                        horizontalArrangement = Arrangement.spacedBy(12.dp),
                    ) {
                        Icon(
                            Icons.Outlined.Fingerprint,
                            contentDescription = null,
                            tint = MaterialTheme.colorScheme.primary,
                        )
                        Column(Modifier.weight(1f)) {
                            Text("充值身份验证", style = MaterialTheme.typography.titleMedium)
                            Text(
                                "开启后进入充值和最终扣款前需要指纹或锁屏凭据，默认关闭",
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                style = MaterialTheme.typography.bodySmall,
                            )
                        }
                        Switch(
                            checked = biometricEnabled,
                            onCheckedChange = viewModel::setBiometricPaymentsEnabled,
                        )
                    }
                }
            }
            item {
                Card(
                    colors = CardDefaults.cardColors(
                        containerColor = MaterialTheme.colorScheme.surface,
                    ),
                ) {
                    Column(Modifier.fillMaxWidth().padding(16.dp)) {
                        Text("学校账号", style = MaterialTheme.typography.titleMedium)
                        Text(
                            session?.sno.orEmpty(),
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                        val days = session?.let {
                            (it.secondsLeft() / 86_400).toInt()
                        } ?: 0
                        Text(
                            if (session == null) "未登录" else "登录状态约剩 $days 天",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                        OutlinedButton(
                            onClick = viewModel::logout,
                            modifier = Modifier.padding(top = 12.dp),
                        ) {
                            Text("退出登录")
                        }
                    }
                }
            }
            item {
                Card(
                    colors = CardDefaults.cardColors(
                        containerColor = MaterialTheme.colorScheme.surface,
                    ),
                ) {
                    Row(
                        modifier = Modifier.fillMaxWidth().padding(16.dp),
                        horizontalArrangement = Arrangement.SpaceBetween,
                    ) {
                        Column(Modifier.weight(1f)) {
                            Text("系统通知", style = MaterialTheme.typography.titleMedium)
                            Text(
                                "低余额、每日余额和登录失效提醒",
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                style = MaterialTheme.typography.bodySmall,
                            )
                        }
                        IconButton(onClick = {
                            val intent = Intent(Settings.ACTION_APP_NOTIFICATION_SETTINGS)
                                .putExtra(Settings.EXTRA_APP_PACKAGE, context.packageName)
                            context.startActivity(intent)
                        }) {
                            Icon(Icons.Outlined.Notifications, contentDescription = "通知设置")
                        }
                    }
                }
            }
            item {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween,
                ) {
                    Text("监控宿舍", style = MaterialTheme.typography.titleMedium)
                    TextButton(onClick = onAddTarget) {
                        Icon(Icons.Outlined.Add, contentDescription = null)
                        Text("添加")
                    }
                }
            }
            items(targets, key = { it.key }) { target ->
                Card(
                    colors = CardDefaults.cardColors(
                        containerColor = MaterialTheme.colorScheme.surface,
                    ),
                ) {
                    Row(
                        modifier = Modifier.fillMaxWidth().padding(14.dp),
                        horizontalArrangement = Arrangement.spacedBy(4.dp),
                    ) {
                        Column(Modifier.weight(1f)) {
                            Text(target.label, style = MaterialTheme.typography.titleSmall)
                            Text(
                                "${target.pollIntervalMinutes} 分钟 · ${target.notifyMode.wireValue}",
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                style = MaterialTheme.typography.bodySmall,
                            )
                        }
                        IconButton(onClick = { onEditTarget(target) }) {
                            Icon(Icons.Outlined.Edit, contentDescription = "编辑")
                        }
                        IconButton(onClick = { pendingDelete = target }) {
                            Icon(Icons.Outlined.Delete, contentDescription = "删除")
                        }
                    }
                }
            }
            if (error.isNotBlank()) {
                item {
                    Text(error, color = MaterialTheme.colorScheme.error)
                }
            }
            item {
                Button(onClick = onAddTarget, modifier = Modifier.fillMaxWidth()) {
                    Icon(Icons.Outlined.Add, contentDescription = null)
                    Text("添加监控宿舍", modifier = Modifier.padding(start = 8.dp))
                }
            }
            item {
                Text(
                    "本应用不使用项目服务器。所有学校请求、历史读数和通知设置均保存在当前设备。",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }

    pendingDelete?.let { target ->
        AlertDialog(
            onDismissRequest = { pendingDelete = null },
            title = { Text("删除宿舍") },
            text = { Text("删除 ${target.label} 的配置和历史读数？") },
            confirmButton = {
                TextButton(onClick = {
                    pendingDelete = null
                    scope.launch {
                        viewModel.deleteTarget(target).onFailure {
                            error = it.message ?: "删除失败"
                        }
                    }
                }) {
                    Text("删除")
                }
            },
            dismissButton = {
                TextButton(onClick = { pendingDelete = null }) { Text("取消") }
            },
        )
    }
}
