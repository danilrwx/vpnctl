package dev.vpnctl.app

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Shapes
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/**
 * The palette comes from DESIGN.md, which took it from the mockups: GitHub Dark,
 * one screen background, one card surface and a soft tint per state. There is no
 * light variant on purpose — the designs are drawn for this one, and a second
 * palette nobody drew is how screens start to drift.
 */
val Canvas = Color(0xFF0E1116)
val Surface1 = Color(0xFF161B22)
val Surface2 = Color(0xFF1C2128)
val Edge = Color(0xFF21262D)
val Ink = Color(0xFFE6EDF3)
val Muted = Color(0xFF8B949E)
val Accent = Color(0xFF1F6FEB)
val AccentSoft = Color(0xFF1C3A60)
val OnAccentSoft = Color(0xFF9ECBFF)
val Link = Color(0xFF58A6FF)

val Ok = Color(0xFF3FB950)
val OkSoft = Color(0xFF1B3A26)
val OkText = Color(0xFF56D364)
val Warn = Color(0xFFF2CC60)
val WarnSoft = Color(0xFF2B2312)
val Danger = Color(0xFFF85149)
val DangerSoft = Color(0xFF3D1D1D)
val DangerText = Color(0xFFFF7B72)

val Inter = FontFamily(
    Font(R.font.inter_regular, FontWeight.Normal),
    Font(R.font.inter_medium, FontWeight.Medium),
    Font(R.font.inter_semibold, FontWeight.SemiBold),
    Font(R.font.inter_bold, FontWeight.Bold),
)

val Mono = FontFamily(
    Font(R.font.jetbrains_mono_regular, FontWeight.Normal),
    Font(R.font.jetbrains_mono_medium, FontWeight.Medium),
)

private fun sans(size: Int, line: Int, weight: FontWeight, tracking: Double = 0.0) = TextStyle(
    fontFamily = Inter,
    fontWeight = weight,
    fontSize = size.sp,
    lineHeight = line.sp,
    letterSpacing = tracking.sp,
)

private val AppTypography = Typography(
    displayLarge = sans(28, 34, FontWeight.Bold, -0.5), // screen titles
    headlineMedium = sans(22, 28, FontWeight.SemiBold, -0.2), // a person's name
    titleLarge = sans(18, 23, FontWeight.Medium), // list rows
    titleMedium = sans(16, 21, FontWeight.Medium), // section titles
    titleSmall = sans(15, 20, FontWeight.Medium),
    bodyLarge = sans(15, 21, FontWeight.Normal),
    bodyMedium = sans(14, 19, FontWeight.Normal),
    bodySmall = sans(13, 18, FontWeight.Normal),
    labelMedium = sans(12, 16, FontWeight.Medium),
    labelSmall = sans(11, 14, FontWeight.Medium),
)

private val AppShapes = Shapes(
    extraSmall = androidx.compose.foundation.shape.RoundedCornerShape(8.dp),
    small = androidx.compose.foundation.shape.RoundedCornerShape(12.dp),
    medium = androidx.compose.foundation.shape.RoundedCornerShape(14.dp),
    large = androidx.compose.foundation.shape.RoundedCornerShape(16.dp),
    extraLarge = androidx.compose.foundation.shape.RoundedCornerShape(24.dp),
)

private val AppColors = darkColorScheme(
    primary = Accent,
    onPrimary = Color.White,
    primaryContainer = AccentSoft,
    onPrimaryContainer = OnAccentSoft,
    background = Canvas,
    onBackground = Ink,
    surface = Surface1,
    onSurface = Ink,
    surfaceVariant = Surface2,
    onSurfaceVariant = Muted,
    outline = Edge,
    outlineVariant = Edge,
    error = Danger,
    onError = Color.White,
    errorContainer = DangerSoft,
    onErrorContainer = DangerText,
)

/** The designs are dark only: a phone in light mode still gets the mockups. */
@Composable
fun VpnctlTheme(content: @Composable () -> Unit) {
    MaterialTheme(colorScheme = AppColors, typography = AppTypography, shapes = AppShapes, content = content)
}

/** mono is for anything the operator may transcribe by hand. */
fun monoStyle(base: TextStyle, weight: FontWeight = FontWeight.Normal) =
    base.copy(fontFamily = Mono, fontWeight = weight)
