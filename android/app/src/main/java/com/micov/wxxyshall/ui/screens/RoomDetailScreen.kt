package com.micov.wxxyshall.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.Payments
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.micov.wxxyshall.domain.Target
import com.micov.wxxyshall.ui.AppViewModel
import com.micov.wxxyshall.ui.components.BalanceChart
import com.micov.wxxyshall.ui.components.BalancePill
import com.micov.wxxyshall.ui.components.EmptyBlock
import com.micov.wxxyshall.ui.components.PageTopBar
import com.micov.wxxyshall.ui.components.formatBalance
import com.micov.wxxyshall.ui.components.formatFullDateTime

@Composable
fun RoomDetailScreen(
    viewModel: AppViewModel,
    target: Target?,
    onBack: () -> Unit,
    onEdit: () -> Unit,
    onRecharge: () -> Unit,
) {
    Scaffold(
        topBar = {
            PageTopBar(
                title = target?.label ?: "宿舍详情",
                onBack = onBack,
                actions = {
                    if (target != null) {
                        IconButton(onClick = onEdit) {
                            Icon(Icons.Outlined.Edit, contentDescription = "编辑")
                        }
                    }
                },
            )
        },
    ) { padding ->
        if (target == null) {
            EmptyBlock("找不到该宿舍", Modifier.padding(padding))
            return@Scaffold
        }
        val flow = remember(target.key) { viewModel.readings(target.key) }
        val readings by flow.collectAsStateWithLifecycle(emptyList())
        val latest = readings.lastOrNull()
        var days by remember { mutableIntStateOf(0) }
        val cutoff = if (days > 0) {
            System.currentTimeMillis() / 1000 - days * 86_400L
        } else {
            0L
        }
        val visibleReadings = remember(readings, cutoff) {
            if (cutoff == 0L) readings else readings.filter { it.epoch >= cutoff }
        }
        LazyColumn(
            modifier = Modifier.fillMaxSize().padding(padding),
            contentPadding = PaddingValues(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            item {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween,
                ) {
                    Column {
                        Text(
                            "${formatBalance(latest?.surplusCharge)} kWh",
                            style = MaterialTheme.typography.headlineMedium,
                        )
                        Text(
                            formatFullDateTime(latest?.epoch ?: 0),
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            style = MaterialTheme.typography.bodySmall,
                        )
                    }
                    BalancePill(latest?.surplusCharge, target.lowBalanceThreshold)
                }
            }
            item {
                Row(
                    modifier = Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()),
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    listOf(1 to "1天", 3 to "3天", 7 to "7天", 30 to "30天", 0 to "全部")
                        .forEach { (value, label) ->
                            FilterChip(
                                selected = days == value,
                                onClick = { days = value },
                                label = { Text(label) },
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
                        Text("剩余电量曲线", style = MaterialTheme.typography.titleMedium)
                        Spacer(Modifier.height(8.dp))
                        BalanceChart(visibleReadings.takeLast(500))
                    }
                }
            }
            item {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    Metric("采样点", visibleReadings.size.toString(), Modifier.weight(1f))
                    Metric(
                        "总用电量",
                        latest?.totalUsage?.let { formatBalance(it) } ?: "—",
                        Modifier.weight(1f),
                    )
                }
            }
            item {
                Button(onClick = onRecharge, modifier = Modifier.fillMaxWidth()) {
                    Icon(Icons.Outlined.Payments, contentDescription = null)
                    Text("校园卡充值", modifier = Modifier.padding(start = 8.dp))
                }
            }
            item {
                Text("最近读数", style = MaterialTheme.typography.titleMedium)
            }
            if (visibleReadings.isEmpty()) {
                item { EmptyBlock("暂无读数") }
            } else {
                items(visibleReadings.takeLast(20).reversed(), key = { it.epoch }) { reading ->
                    Card(
                        colors = CardDefaults.cardColors(
                            containerColor = MaterialTheme.colorScheme.surface,
                        ),
                    ) {
                        Row(
                            modifier = Modifier.fillMaxWidth().padding(14.dp),
                            horizontalArrangement = Arrangement.SpaceBetween,
                        ) {
                            Text(
                                formatFullDateTime(reading.epoch),
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                            Text(
                                "${formatBalance(reading.surplusCharge)} kWh",
                                style = MaterialTheme.typography.bodyMedium,
                            )
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun Metric(label: String, value: String, modifier: Modifier = Modifier) {
    Card(
        modifier = modifier,
        colors = CardDefaults.cardColors(
            containerColor = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.6f),
        ),
    ) {
        Column(Modifier.padding(14.dp)) {
            Text(
                label,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                style = MaterialTheme.typography.bodySmall,
            )
            Text(value, style = MaterialTheme.typography.titleLarge)
        }
    }
}
