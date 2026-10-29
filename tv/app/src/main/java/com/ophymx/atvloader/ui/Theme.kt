@file:OptIn(ExperimentalTvMaterial3Api::class)

package com.ophymx.atvloader.ui

import androidx.tv.material3.ExperimentalTvMaterial3Api
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.tv.material3.MaterialTheme
import androidx.tv.material3.darkColorScheme

val Ink = Color(0xFF0F1216)
val Panel = Color(0xFF1A1F26)
val PanelRaised = Color(0xFF242B34)
val Fog = Color(0xFFD9DEE5)
val Mist = Color(0xFF8B949F)
val Lime = Color(0xFFB5D96A)
val Amber = Color(0xFFE8B04B)
val Coral = Color(0xFFE9705C)

@Composable
fun AtvLoaderTheme(content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = darkColorScheme(
            primary = Lime,
            onPrimary = Ink,
            secondary = Amber,
            onSecondary = Ink,
            background = Ink,
            onBackground = Fog,
            surface = Panel,
            onSurface = Fog,
            surfaceVariant = PanelRaised,
            onSurfaceVariant = Mist,
            error = Coral,
            onError = Ink,
            border = Color(0xFF3A434E),
        ),
        content = content,
    )
}
