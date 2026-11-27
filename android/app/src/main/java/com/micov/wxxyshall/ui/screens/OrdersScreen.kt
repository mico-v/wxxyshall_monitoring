package com.micov.wxxyshall.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.micov.wxxyshall.domain.RechargeOrder
import com.micov.wxxyshall.ui.AppViewModel
import com.micov.wxxyshall.ui.components.EmptyBlock
import com.micov.wxxyshall.ui.components.PageTopBar
import com.micov.wxxyshall.ui.components.formatBalance
import com.micov.wxxyshall.ui.components.orderTail
import java.util.Locale

@Composable
fun OrdersScreen(viewModel: AppViewModel, onDashboard: () -> Unit) {
    var status by remember { mutableIntStateOf(1) }
    var loading by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf("") }
    var orders by remember { mutableStateOf(emptyList<RechargeOrder>()) }

    LaunchedEffect(status) {
        loading = true
        error = ""
        viewModel.loadOrders(status)
            .onSuccess { orders = it }
            .onFailure {
                orders = emptyList()
                error = it.message ?: "订单查询失败"
            }
        loading = false
    }

    Scaffold(topBar = { PageTopBar("学校订单") }) { padding ->
        LazyColumn(
            modifier = Modifier.fillMaxSize().padding(padding),
            contentPadding = PaddingValues(16.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item {
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    FilterChip(
                        selected = status == 1,
                        onClick = { status = 1 },
                        label = { Text("充值记录") },
                    )
                    FilterChip(
                        selected = status == 0,
                        onClick = { status = 0 },
                        label = { Text("待支付") },
                    )
                }
            }
            if (loading) {
                item {
                    CircularProgressIndicator(Modifier.padding(32.dp))
                }
            }
            if (error.isNotBlank()) {
                item { Text(error, color = MaterialTheme.colorScheme.error) }
            }
            if (!loading && error.isBlank() && orders.isEmpty()) {
                item { EmptyBlock("暂无订单") }
            }
            items(orders, key = { it.orderId }) { order ->
                Card(
                    colors = CardDefaults.cardColors(
                        containerColor = MaterialTheme.colorScheme.surface,
                    ),
                ) {
                    Column(Modifier.fillMaxWidth().padding(14.dp)) {
                        Row(
                            modifier = Modifier.fillMaxWidth(),
                            horizontalArrangement = Arrangement.SpaceBetween,
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Text(
                                "¥${String.format(Locale.US, "%.2f", order.actualAmount ?: order.tranAmount)}",
                                style = MaterialTheme.typography.titleMedium,
                            )
                            Text(
                                if (order.status == 0) "待支付" else "已支付",
                                color = if (order.status == 0) {
                                    MaterialTheme.colorScheme.tertiary
                                } else {
                                    MaterialTheme.colorScheme.primary
                                },
                                style = MaterialTheme.typography.labelLarge,
                            )
                        }
                        Text(
                            order.room?.let { "${it.campus} ${it.building} ${it.room}" }
                                ?: order.abstracts.ifBlank { "宿舍购电" },
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            style = MaterialTheme.typography.bodySmall,
                        )
                        Text(
                            "${order.successDate.ifBlank { order.commitDate }} · ${orderTail(order.orderId)}",
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            style = MaterialTheme.typography.bodySmall,
                        )
                    }
                }
            }
        }
    }
}
