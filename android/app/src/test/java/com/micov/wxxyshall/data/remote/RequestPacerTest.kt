package com.micov.wxxyshall.data.remote

import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test

class RequestPacerTest {
    @Test
    fun rateIsConvertedToStrictInterval() {
        assertEquals(60_000L, RequestPacer.intervalFor(1))
        assertEquals(2_000L, RequestPacer.intervalFor(30))
        assertEquals(8_572L, RequestPacer.intervalFor(7))
    }

    @Test
    fun waitsForRemainingInterval() = runTest {
        var now = 1_000L
        val sleeps = mutableListOf<Long>()
        val pacer = RequestPacer(
            perMinute = 30,
            clock = { now },
            sleep = { millis ->
                sleeps += millis
                now += millis
            },
        )

        pacer.await()
        now += 500
        pacer.await()

        assertEquals(listOf(1_500L), sleeps)
    }
}
