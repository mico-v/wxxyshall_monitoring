package com.micov.wxxyshall.data.remote

import com.micov.wxxyshall.domain.SchoolException
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test

class PasswordKeyboardTest {
    private val identity = KeyboardData(
        uuid = "keyboard-id",
        numberKeyboard = "0123456789",
        lowerLetterKeyboard = "qwertyuiopasdfghjklzxcvbnm",
        upperLetterKeyboard = "QWERTYUIOPASDFGHJKLZXCVBNM",
        symbolKeyboard = "*\\-[]{}/!,<>?~&@#.:+|`%'\$;^\"_",
    )

    @Test
    fun identityKeyboardKeepsPassword() {
        assertEquals("Pass#1", mapKeyboardPassword("Pass#1", identity))
    }

    @Test
    fun shuffledKeyboardMapsEachCharacterByPosition() {
        val shifted = identity.copy(
            numberKeyboard = "9876543210",
            lowerLetterKeyboard = "azertyuiopqsdfghjklmwxcvbn",
            upperLetterKeyboard = "AZERTYUIOPQSDFGHJKLMWXCVBN",
        )
        assertEquals("a8", mapKeyboardPassword("q1", shifted))
    }

    @Test
    fun unsupportedCharacterIsRejected() {
        assertThrows(SchoolException::class.java) {
            mapKeyboardPassword("密码", identity)
        }
    }

    @Test
    fun incompleteKeyboardIsRejected() {
        assertThrows(SchoolException::class.java) {
            mapKeyboardPassword("a", identity.copy(lowerLetterKeyboard = "abc"))
        }
    }
}
