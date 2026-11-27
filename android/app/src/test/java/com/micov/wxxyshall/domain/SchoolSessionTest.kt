package com.micov.wxxyshall.domain

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class SchoolSessionTest {
    @Test
    fun expiryUsesSafetyBuffer() {
        val session = SchoolSession(
            accessToken = "token",
            expiresIn = 1_000,
            loginTime = 10_000,
            sno = "20260001",
        )

        assertFalse(session.isExpired(bufferSeconds = 600, nowSeconds = 10_399))
        assertTrue(session.isExpired(bufferSeconds = 600, nowSeconds = 10_400))
        assertEquals(400, session.secondsLeft(nowSeconds = 10_600))
    }
}
