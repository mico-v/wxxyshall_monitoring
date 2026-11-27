package com.micov.wxxyshall.notifications

import android.Manifest
import android.annotation.SuppressLint
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat
import com.micov.wxxyshall.MainActivity
import com.micov.wxxyshall.R
import com.micov.wxxyshall.domain.Reading
import com.micov.wxxyshall.domain.Target
import java.util.Locale

class NotificationCoordinator(private val context: Context) {
    private val manager = NotificationManagerCompat.from(context)

    fun createChannels() {
        val system = context.getSystemService(NotificationManager::class.java)
        system.createNotificationChannels(
            listOf(
                NotificationChannel(
                    CHANNEL_ALERT,
                    "低余额提醒",
                    NotificationManager.IMPORTANCE_HIGH,
                ).apply {
                    description = "宿舍余额首次进入低值区间时提醒"
                },
                NotificationChannel(
                    CHANNEL_DAILY,
                    "每日电费",
                    NotificationManager.IMPORTANCE_DEFAULT,
                ).apply {
                    description = "按宿舍设定时间发送每日余额"
                },
                NotificationChannel(
                    CHANNEL_SYSTEM,
                    "采集状态",
                    NotificationManager.IMPORTANCE_DEFAULT,
                ).apply {
                    description = "登录失效和后台采集异常"
                },
            ),
        )
    }

    @SuppressLint("MissingPermission")
    fun notifyReading(target: Target, reading: Reading, kind: NotificationKind): Boolean {
        if (!canNotify()) return false
        val balance = reading.surplusCharge ?: return false
        val channel = if (kind == NotificationKind.LOW_BALANCE) CHANNEL_ALERT else CHANNEL_DAILY
        val title = if (kind == NotificationKind.LOW_BALANCE) {
            "${target.label} 余额偏低"
        } else {
            "${target.label} 今日余额"
        }
        val content = String.format(Locale.US, "剩余 %.2f kWh", balance)
        val notification = NotificationCompat.Builder(context, channel)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(title)
            .setContentText(content)
            .setStyle(NotificationCompat.BigTextStyle().bigText(content))
            .setContentIntent(roomPendingIntent(target))
            .setAutoCancel(true)
            .setVisibility(NotificationCompat.VISIBILITY_PRIVATE)
            .setOnlyAlertOnce(true)
            .build()
        return runCatching {
            manager.notify(("$channel|${target.key}").hashCode(), notification)
        }.isSuccess
    }

    @SuppressLint("MissingPermission")
    fun notifyAuthExpired() {
        if (!canNotify()) return
        val notification = NotificationCompat.Builder(context, CHANNEL_SYSTEM)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle("学校登录已失效")
            .setContentText("打开应用重新登录后继续后台采集")
            .setContentIntent(launchPendingIntent())
            .setAutoCancel(true)
            .setVisibility(NotificationCompat.VISIBILITY_PRIVATE)
            .build()
        runCatching { manager.notify(AUTH_NOTIFICATION_ID, notification) }
    }

    fun clearAuthExpired() {
        manager.cancel(AUTH_NOTIFICATION_ID)
    }

    fun canNotify(): Boolean {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return true
        return ContextCompat.checkSelfPermission(
            context,
            Manifest.permission.POST_NOTIFICATIONS,
        ) == PackageManager.PERMISSION_GRANTED
    }

    private fun roomPendingIntent(target: Target): PendingIntent {
        val uri = Uri.Builder()
            .scheme("wxxyshall")
            .authority("room")
            .appendQueryParameter("campus", target.campus)
            .appendQueryParameter("building", target.building)
            .appendQueryParameter("room", target.room)
            .build()
        val intent = Intent(Intent.ACTION_VIEW, uri, context, MainActivity::class.java)
            .addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP)
        return PendingIntent.getActivity(
            context,
            target.key.hashCode(),
            intent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
    }

    private fun launchPendingIntent(): PendingIntent {
        val intent = context.packageManager.getLaunchIntentForPackage(context.packageName)
            ?: Intent(Intent.ACTION_MAIN).setPackage(context.packageName)
        return PendingIntent.getActivity(
            context,
            AUTH_NOTIFICATION_ID,
            intent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
    }

    private companion object {
        const val CHANNEL_ALERT = "low-balance"
        const val CHANNEL_DAILY = "daily-balance"
        const val CHANNEL_SYSTEM = "collection-status"
        const val AUTH_NOTIFICATION_ID = 7001
    }
}
