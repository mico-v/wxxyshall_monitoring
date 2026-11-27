package com.micov.wxxyshall.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

private val LightColors = lightColorScheme(
    primary = Color(0xFF0B6B4F),
    onPrimary = Color.White,
    primaryContainer = Color(0xFFCEF2E2),
    onPrimaryContainer = Color(0xFF002118),
    secondary = Color(0xFF315E8A),
    onSecondary = Color.White,
    secondaryContainer = Color(0xFFD5E6F8),
    onSecondaryContainer = Color(0xFF001D33),
    tertiary = Color(0xFF8A5700),
    tertiaryContainer = Color(0xFFFFDDB0),
    background = Color(0xFFF7F9F7),
    surface = Color(0xFFFCFDFB),
    surfaceVariant = Color(0xFFE3E9E4),
    error = Color(0xFFB3261E),
)

private val DarkColors = darkColorScheme(
    primary = Color(0xFF8ED7B9),
    onPrimary = Color(0xFF003828),
    primaryContainer = Color(0xFF00513C),
    onPrimaryContainer = Color(0xFFCEF2E2),
    secondary = Color(0xFF9FCBF8),
    onSecondary = Color(0xFF003353),
    secondaryContainer = Color(0xFF174A71),
    onSecondaryContainer = Color(0xFFD5E6F8),
    tertiary = Color(0xFFFFB95C),
    tertiaryContainer = Color(0xFF683D00),
    background = Color(0xFF101412),
    surface = Color(0xFF151A17),
    surfaceVariant = Color(0xFF404943),
    error = Color(0xFFFFB4AB),
)

@Composable
fun WxxyshallTheme(content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = if (isSystemInDarkTheme()) DarkColors else LightColors,
        content = content,
    )
}
