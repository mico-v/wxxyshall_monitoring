package com.micov.wxxyshall.ui.screens

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.micov.wxxyshall.domain.Target
import com.micov.wxxyshall.ui.AppViewModel
import com.micov.wxxyshall.ui.components.BalancePill
import com.micov.wxxyshall.ui.components.EmptyBlock
import com.micov.wxxyshall.ui.components.PageTopBar
import com.micov.wxxyshall.ui.components.formatBalance
import com.micov.wxxyshall.ui.components.formatDateTime

@Composable
fun DashboardScreen(
    viewModel: AppViewModel,
    onRoom: (Target) -> Unit,
    onAdd: () -> Unit,
    onSettings: () -> Unit,
) {
    val targets by viewModel.targets.collectAsStateWithLifecycle()
    val latest by viewModel.latestReadings.collectAsStateWithLifecycle()
    val lowCount = targets.count { target ->
        val value = latest[target.key]?.surplusCharge
        value != null && value <= target.lowBalanceThreshold
    }
    Scaffold(
        topBar = {
            PageTopBar(
                title = "宿舍电费",
                actions = {
                    IconButton(onClick = viewModel::collectNow) {
                        Icon(Icons.Outlined.Refresh, contentDescription = "立即采集")
                    }
                    IconButton(onClick = onSettings) {
                        Icon(Icons.Outlined.Settings, contentDescription = "设置")
                    }
                },
            )
        },
        floatingActionButton = {
            androidx.compose.material3.ExtendedFloatingActionButton(onClick = onAdd) {
                Icon(Icons.Outlined.Add, contentDescription = null)
                Text("添加宿舍", modifier = Modifier.padding(start = 8.dp))
            }
        },
    ) { padding ->
        LazyColumn(
            modifier = Modifier.fillMaxSize().padding(padding),
            contentPadding = PaddingValues(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            item {
                Surface(
                    color = if (lowCount > 0) {
                        MaterialTheme.colorScheme.errorContainer
                    } else {
                        MaterialTheme.colorScheme.primaryContainer
                    },
                    shape = MaterialTheme.shapes.medium,
                ) {
                    Column(Modifier.fillMaxWidth().padding(16.dp)) {
                        Text(
                            text = when {
                                targets.isEmpty() -> "尚未添加宿舍"
                                lowCount == 0 -> "全部 ${targets.size} 间宿舍余额正常"
                                else -> "$lowCount / ${targets.size} 间宿舍余额偏低"
                            },
                            style = MaterialTheme.typography.titleMedium,
                        )
                        Spacer(Modifier.height(4.dp))
                        Text(
                            "后台每 15 分钟检查到期宿舍",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }
            if (targets.isEmpty()) {
                item {
                    EmptyBlock("先添加一间宿舍")
                }
            }
            items(targets, key = { it.key }) { target ->
                val reading = latest[target.key]
                Card(
                    modifier = Modifier.fillMaxWidth().clickable { onRoom(target) },
                    colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surface),
                ) {
                    Column(Modifier.padding(16.dp)) {
                        Row(
                            modifier = Modifier.fillMaxWidth(),
                            horizontalArrangement = Arrangement.SpaceBetween,
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Column(Modifier.weight(1f)) {
                                Text(target.label, style = MaterialTheme.typography.titleMedium)
                                Text(
                                    "${target.campus} / ${target.building} / ${target.room}",
                                    style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                            }
                            BalancePill(reading?.surplusCharge, target.lowBalanceThreshold)
                        }
                        Spacer(Modifier.height(14.dp))
                        Row(
                            modifier = Modifier.fillMaxWidth(),
                            horizontalArrangement = Arrangement.SpaceBetween,
                            verticalAlignment = Alignment.Bottom,
                        ) {
                            Text(
                                "${formatBalance(reading?.surplusCharge)} kWh",
                                style = MaterialTheme.typography.headlineSmall,
                            )
                            Text(
                                formatDateTime(reading?.epoch ?: 0),
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                }
            }
        }
    }
}
