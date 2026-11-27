package com.micov.wxxyshall.work

import android.content.Context
import androidx.work.Constraints
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.workDataOf
import java.util.concurrent.TimeUnit

class CollectionScheduler(private val context: Context) {
    private val workManager = WorkManager.getInstance(context)
    private val constraints = Constraints.Builder()
        .setRequiredNetworkType(NetworkType.CONNECTED)
        .build()

    fun schedule() {
        val request = PeriodicWorkRequestBuilder<CollectionWorker>(15, TimeUnit.MINUTES)
            .setConstraints(constraints)
            .addTag(TAG_PERIODIC)
            .build()
        workManager.enqueueUniquePeriodicWork(
            PERIODIC_NAME,
            ExistingPeriodicWorkPolicy.UPDATE,
            request,
        )
    }

    fun collectNow() {
        val request = OneTimeWorkRequestBuilder<CollectionWorker>()
            .setConstraints(constraints)
            .setInputData(workDataOf(CollectionWorker.KEY_FORCE to true))
            .addTag(TAG_MANUAL)
            .build()
        workManager.enqueueUniqueWork(MANUAL_NAME, ExistingWorkPolicy.KEEP, request)
    }

    private companion object {
        const val PERIODIC_NAME = "wxxyshall-periodic-collection"
        const val MANUAL_NAME = "wxxyshall-manual-collection"
        const val TAG_PERIODIC = "periodic-collection"
        const val TAG_MANUAL = "manual-collection"
    }
}
