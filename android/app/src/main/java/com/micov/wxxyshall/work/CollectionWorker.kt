package com.micov.wxxyshall.work

import android.content.Context
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import com.micov.wxxyshall.WxxyshallApp

class CollectionWorker(
    appContext: Context,
    params: WorkerParameters,
) : CoroutineWorker(appContext, params) {
    override suspend fun doWork(): Result {
        val app = applicationContext as WxxyshallApp
        val result = if (inputData.getBoolean(KEY_FORCE, false)) {
            app.container.collector.collectAll()
        } else {
            app.container.collector.collectDue()
        }
        return when {
            result.busy -> Result.retry()
            result.authExpired -> Result.success()
            result.failed > 0 -> Result.retry()
            else -> Result.success()
        }
    }

    companion object {
        const val KEY_FORCE = "force"
    }
}
