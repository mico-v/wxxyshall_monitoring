package com.micov.wxxyshall.data.remote

import com.micov.wxxyshall.domain.SchoolErrorKind
import com.micov.wxxyshall.domain.SchoolException
import kotlinx.serialization.Serializable

@Serializable
data class KeyboardData(
    val uuid: String,
    val numberKeyboard: String,
    val lowerLetterKeyboard: String,
    val upperLetterKeyboard: String,
    val symbolKeyboard: String,
)

private const val NUMBER_LAYOUT = "0123456789"
private const val LOWER_LAYOUT = "qwertyuiopasdfghjklzxcvbnm"
private const val UPPER_LAYOUT = "QWERTYUIOPASDFGHJKLZXCVBNM"
private const val SYMBOL_LAYOUT = "*\\-[]{}/!,<>?~&@#.:+|`%'\$;^\"_"

fun mapKeyboardPassword(password: String, keyboard: KeyboardData): String {
    val numbers = keyboard.numberKeyboard.toList()
    val lower = keyboard.lowerLetterKeyboard.toList()
    val upper = keyboard.upperLetterKeyboard.toList()
    val symbols = keyboard.symbolKeyboard.toList()
    if (numbers.size < 10 ||
        lower.size < 26 ||
        upper.size < 26 ||
        symbols.size < SYMBOL_LAYOUT.length
    ) {
        throw SchoolException(SchoolErrorKind.BAD_RESPONSE, "学校安全键盘映射不完整")
    }

    return buildString(password.length) {
        password.forEach { char ->
            val (mapping, index) = when {
                char.isDigit() -> numbers to NUMBER_LAYOUT.indexOf(char)
                char in 'a'..'z' -> lower to LOWER_LAYOUT.indexOf(char)
                char in 'A'..'Z' -> upper to UPPER_LAYOUT.indexOf(char)
                else -> symbols to SYMBOL_LAYOUT.indexOf(char)
            }
            if (index < 0 || index >= mapping.size) {
                throw SchoolException(
                    SchoolErrorKind.CREDENTIALS,
                    "密码包含学校安全键盘不支持的字符",
                )
            }
            append(mapping[index])
        }
    }
}
