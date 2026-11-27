package com.micov.wxxyshall.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ArrowDropDown
import androidx.compose.material3.Button
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.micov.wxxyshall.domain.NotificationMode
import com.micov.wxxyshall.domain.Option
import com.micov.wxxyshall.domain.Target
import com.micov.wxxyshall.domain.TargetDraft
import com.micov.wxxyshall.ui.AppViewModel
import com.micov.wxxyshall.ui.components.PageTopBar
import kotlinx.coroutines.launch

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AddRoomScreen(
    viewModel: AppViewModel,
    existing: Target?,
    onBack: () -> Unit,
    onSaved: () -> Unit,
) {
    val scope = rememberCoroutineScope()
    var campuses by remember { mutableStateOf(emptyList<Option>()) }
    var buildings by remember { mutableStateOf(emptyList<Option>()) }
    var rooms by remember { mutableStateOf(emptyList<Option>()) }
    var campus by remember { mutableStateOf(existing?.campus.orEmpty()) }
    var building by remember { mutableStateOf(existing?.building.orEmpty()) }
    var room by remember { mutableStateOf(existing?.room.orEmpty()) }
    var label by remember { mutableStateOf(existing?.label.orEmpty()) }
    var interval by remember { mutableStateOf((existing?.pollIntervalMinutes ?: 60).toString()) }
    var notifyMode by remember { mutableStateOf(existing?.notifyMode ?: NotificationMode.ALERT) }
    var notifyTime by remember { mutableStateOf(existing?.notifyTime ?: "08:00") }
    var threshold by remember { mutableStateOf((existing?.lowBalanceThreshold ?: 10.0).toString()) }
    var loading by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf("") }

    LaunchedEffect(Unit) {
        loading = true
        viewModel.loadCampuses()
            .onSuccess { campuses = it }
            .onFailure { error = it.message ?: "校区加载失败" }
        if (existing != null) {
            viewModel.loadBuildings(existing.campus).onSuccess { buildings = it }
            viewModel.loadRooms(existing.campus, existing.building).onSuccess { rooms = it }
        }
        loading = false
    }

    LaunchedEffect(campus) {
        if (campus.isBlank()) return@LaunchedEffect
        if (campus != existing?.campus || buildings.isEmpty()) {
            viewModel.loadBuildings(campus).onSuccess {
                buildings = it
                if (campus != existing?.campus) {
                    building = ""
                    room = ""
                }
            }
        }
    }
    LaunchedEffect(campus, building) {
        if (campus.isBlank() || building.isBlank()) return@LaunchedEffect
        if (campus != existing?.campus || building != existing?.building || rooms.isEmpty()) {
            viewModel.loadRooms(campus, building).onSuccess {
                rooms = it
                if (campus != existing?.campus || building != existing?.building) room = ""
            }
        }
    }

    Scaffold(
        topBar = {
            PageTopBar(if (existing == null) "添加宿舍" else "编辑宿舍", onBack)
        },
    ) { padding ->
        LazyColumn(
            modifier = Modifier.fillMaxSize().padding(padding),
            contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 16.dp, bottom = 48.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            item {
                SelectionField(
                    label = "校区",
                    value = optionName(campuses, campus),
                    options = campuses,
                    onSelected = { campus = it.value },
                )
            }
            item {
                SelectionField(
                    label = "楼栋",
                    value = optionName(buildings, building),
                    options = buildings,
                    enabled = campus.isNotBlank(),
                    onSelected = { building = it.value },
                )
            }
            item {
                SelectionField(
                    label = "房间",
                    value = optionName(rooms, room),
                    options = rooms,
                    enabled = building.isNotBlank(),
                    onSelected = { room = it.value },
                )
            }
            item {
                OutlinedTextField(
                    value = label,
                    onValueChange = { label = it },
                    label = { Text("显示名称") },
                    placeholder = { Text("例如：3 号楼 502") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
            item {
                OutlinedTextField(
                    value = interval,
                    onValueChange = { interval = it.filter(Char::isDigit) },
                    label = { Text("采集周期（分钟）") },
                    supportingText = { Text("Android 后台最短为 15 分钟") },
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
            item {
                Text("通知方式", style = MaterialTheme.typography.labelLarge)
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    NotificationMode.entries.forEach { mode ->
                        FilterChip(
                            selected = notifyMode == mode,
                            onClick = { notifyMode = mode },
                            label = {
                                Text(
                                    when (mode) {
                                        NotificationMode.ALERT -> "低余额"
                                        NotificationMode.DAILY -> "每日"
                                        NotificationMode.NONE -> "关闭"
                                    },
                                )
                            },
                        )
                    }
                }
            }
            item {
                OutlinedTextField(
                    value = threshold,
                    onValueChange = { threshold = it.replace(',', '.') },
                    label = { Text("低余额阈值（kWh）") },
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
            if (notifyMode == NotificationMode.DAILY) {
                item {
                    OutlinedTextField(
                        value = notifyTime,
                        onValueChange = { notifyTime = it },
                        label = { Text("每日通知时间（HH:MM）") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
            }
            if (error.isNotBlank()) {
                item { Text(error, color = MaterialTheme.colorScheme.error) }
            }
            item {
                Button(
                    enabled = !loading &&
                        campus.isNotBlank() &&
                        building.isNotBlank() &&
                        room.isNotBlank(),
                    onClick = {
                        val draft = TargetDraft(
                            campus = campus,
                            building = building,
                            room = room,
                            label = label,
                            pollIntervalMinutes = interval.toIntOrNull() ?: 0,
                            notifyMode = notifyMode,
                            notifyTime = notifyTime,
                            lowBalanceThreshold = threshold.toDoubleOrNull() ?: -1.0,
                        )
                        scope.launch {
                            loading = true
                            error = ""
                            viewModel.saveTarget(existing, draft)
                                .onSuccess { onSaved() }
                                .onFailure { error = it.message ?: "保存失败" }
                            loading = false
                        }
                    },
                    modifier = Modifier.fillMaxWidth().padding(vertical = 20.dp),
                ) {
                    Text(if (loading) "保存中…" else "保存配置")
                }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun SelectionField(
    label: String,
    value: String,
    options: List<Option>,
    enabled: Boolean = true,
    onSelected: (Option) -> Unit,
) {
    var expanded by remember { mutableStateOf(false) }
    ExposedDropdownMenuBox(
        expanded = expanded,
        onExpandedChange = { if (enabled && options.isNotEmpty()) expanded = it },
    ) {
        OutlinedTextField(
            value = value,
            onValueChange = {},
            readOnly = true,
            enabled = enabled,
            label = { Text(label) },
            trailingIcon = {
                androidx.compose.material3.Icon(
                    Icons.Outlined.ArrowDropDown,
                    contentDescription = null,
                )
            },
            modifier = Modifier
                .fillMaxWidth()
                .menuAnchor(),
        )
        ExposedDropdownMenu(
            expanded = expanded,
            onDismissRequest = { expanded = false },
        ) {
            options.forEach { option ->
                DropdownMenuItem(
                    text = { Text(option.name) },
                    onClick = {
                        expanded = false
                        onSelected(option)
                    },
                )
            }
        }
    }
}

private fun optionName(options: List<Option>, value: String): String =
    options.firstOrNull { it.value == value }?.name ?: value
