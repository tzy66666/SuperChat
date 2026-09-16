package com.schat.app.ui.theme

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Typography
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp

val AppTypography = Typography()

private val ColorScheme = lightColorScheme(
    primary = BluePrimary,
    secondary = PurpleAccent,
    background = BgDark,
    surface = SurfaceWhite,
    onPrimary = TextWhite,
    onBackground = TextPrimary,
    onSurface = TextPrimary
)

@Composable
fun SuperChatTheme(content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = ColorScheme,
        typography = AppTypography,
        content = content
    )
}
