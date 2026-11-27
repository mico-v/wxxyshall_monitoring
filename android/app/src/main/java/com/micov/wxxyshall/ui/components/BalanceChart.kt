package com.micov.wxxyshall.ui.components

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.unit.dp
import com.micov.wxxyshall.domain.Reading
import kotlin.math.max

@Composable
fun BalanceChart(
    readings: List<Reading>,
    modifier: Modifier = Modifier,
) {
    val points = readings.mapNotNull { reading ->
        reading.surplusCharge?.let { reading.epoch.toFloat() to it.toFloat() }
    }
    if (points.size < 2) {
        Box(
            modifier = modifier
                .fillMaxWidth()
                .height(220.dp)
                .padding(16.dp),
            contentAlignment = Alignment.Center,
        ) {
            Text("暂无足够曲线数据", color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        return
    }
    val lineColor = MaterialTheme.colorScheme.primary
    val fillColor = lineColor.copy(alpha = 0.12f)
    val gridColor = MaterialTheme.colorScheme.outlineVariant
    Box(modifier = modifier.fillMaxWidth().height(220.dp)) {
        Canvas(Modifier.fillMaxWidth().height(220.dp)) {
            val left = 12.dp.toPx()
            val right = size.width - 12.dp.toPx()
            val top = 12.dp.toPx()
            val bottom = size.height - 20.dp.toPx()
            val minX = points.first().first
            val maxX = points.last().first
            val rawMin = points.minOf { it.second }
            val rawMax = points.maxOf { it.second }
            val extra = max((rawMax - rawMin) * 0.12f, 1f)
            val minY = (rawMin - extra).coerceAtLeast(0f)
            val maxY = rawMax + extra
            val spanX = max(maxX - minX, 1f)
            val spanY = max(maxY - minY, 1f)

            repeat(4) { index ->
                val y = top + (bottom - top) * index / 3f
                drawLine(
                    color = gridColor,
                    start = Offset(left, y),
                    end = Offset(right, y),
                    strokeWidth = 1.dp.toPx(),
                )
            }

            val mapped = points.map { (x, y) ->
                Offset(
                    x = left + (x - minX) / spanX * (right - left),
                    y = bottom - (y - minY) / spanY * (bottom - top),
                )
            }
            val path = Path().apply {
                moveTo(mapped.first().x, mapped.first().y)
                mapped.drop(1).forEach { lineTo(it.x, it.y) }
            }
            val fill = Path().apply {
                moveTo(mapped.first().x, bottom)
                mapped.forEach { lineTo(it.x, it.y) }
                lineTo(mapped.last().x, bottom)
                close()
            }
            drawPath(fill, fillColor)
            drawPath(
                path = path,
                color = lineColor,
                style = Stroke(width = 2.5.dp.toPx(), cap = StrokeCap.Round),
            )
            drawCircle(
                color = lineColor,
                radius = 4.dp.toPx(),
                center = mapped.last(),
            )
        }
    }
}
