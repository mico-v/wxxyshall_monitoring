package com.micov.wxxyshall.data.remote

import kotlinx.coroutines.delay
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlin.math.max

class RequestPacer(
    perMinute: Int = 30,
    private val clock: () -> Long = System::currentTimeMillis,
    private val sleep: suspend (Long) -> Unit = { delay(it) },
) {
    private val mutex = Mutex()
    @Volatile private var intervalMillis: Long = intervalFor(perMinute)
    private var lastRequestAt: Long = 0

    fun setRate(perMinute: Int) {
        intervalMillis = intervalFor(perMinute)
    }

    suspend fun await() {
        mutex.withLock {
            val now = clock()
            val waitFor = if (lastRequestAt == 0L) 0 else {
                max(0, intervalMillis - (now - lastRequestAt))
            }
            if (waitFor > 0) sleep(waitFor)
            lastRequestAt = clock()
        }
    }

    companion object {
        fun intervalFor(perMinute: Int): Long {
            val normalized = perMinute.coerceIn(1, 600)
            return (60_000L + normalized - 1) / normalized
        }
    }
}
