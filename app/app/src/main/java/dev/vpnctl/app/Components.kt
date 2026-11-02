package dev.vpnctl.app

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.PathEffect
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/**
 * The pieces the mockups repeat: a 28pt title with a muted count, pills for
 * filters, 52pt avatars, cards, a stat trio and the bottom bar. Screens compose
 * these instead of styling Material widgets one by one, which is how the app
 * drifted away from the design in the first place.
 */

@Composable
fun ScreenHeader(
    title: String,
    subtitle: String? = null,
    onBack: (() -> Unit)? = null,
    stacked: Boolean = false,
    backIcon: ImageVector = Icons.AutoMirrored.Outlined.ArrowBack,
    actions: @Composable RowScope.() -> Unit = {},
) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(start = 16.dp, end = 8.dp, top = 12.dp, bottom = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (onBack != null) {
            IconAction(backIcon, onBack, "Back")
        }

        if (stacked) {
            Column(modifier = Modifier.weight(1f)) {
                Text(title, style = MaterialTheme.typography.displayLarge, maxLines = 1, overflow = TextOverflow.Ellipsis)

                if (!subtitle.isNullOrEmpty()) {
                    Text(
                        subtitle,
                        style = MaterialTheme.typography.bodySmall,
                        color = Muted,
                        maxLines = 1,
                        modifier = Modifier.padding(top = 2.dp),
                    )
                }
            }
        } else {
            Row(modifier = Modifier.weight(1f), verticalAlignment = Alignment.Bottom) {
                Text(title, style = MaterialTheme.typography.displayLarge, maxLines = 1, overflow = TextOverflow.Ellipsis)

                if (!subtitle.isNullOrEmpty()) {
                    Text(
                        subtitle,
                        style = MaterialTheme.typography.bodySmall,
                        color = Muted,
                        maxLines = 1,
                        modifier = Modifier.padding(start = 8.dp, bottom = 5.dp),
                    )
                }
            }
        }

        actions()
    }
}

/** plural is the one grammar the screens need: 1 entry, 2 entries. */
fun plural(n: Int, one: String, many: String): String = if (n == 1) one else many

@Composable
fun IconAction(icon: ImageVector, onClick: () -> Unit, description: String, tint: Color = Muted) {
    IconButton(onClick = onClick, modifier = Modifier.size(40.dp)) {
        Icon(icon, contentDescription = description, tint = tint, modifier = Modifier.size(22.dp))
    }
}

@Composable
fun SearchPill(value: String, onValueChange: (String) -> Unit, placeholder: String) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .height(48.dp)
            .clip(RoundedCornerShape(24.dp))
            .background(Surface2)
            .padding(horizontal = 16.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(Icons.Outlined.Search, contentDescription = null, tint = Muted, modifier = Modifier.size(20.dp))
        Spacer(Modifier.width(10.dp))
        Box(modifier = Modifier.weight(1f)) {
            if (value.isEmpty()) {
                Text(placeholder, style = MaterialTheme.typography.bodyLarge, color = Muted)
            }

            BasicTextField(
                value = value,
                onValueChange = onValueChange,
                singleLine = true,
                textStyle = TextStyle(fontFamily = Inter, fontSize = 15.sp, color = Ink),
                cursorBrush = SolidColor(Accent),
                modifier = Modifier.fillMaxWidth(),
            )
        }
    }
}

@Composable
fun Chip(
    label: String,
    onClick: () -> Unit,
    selected: Boolean = false,
    warn: Boolean = false,
    check: Boolean = false,
) {
    val fg = when {
        selected -> OnAccentSoft
        warn -> Warn
        else -> Ink
    }

    Row(
        modifier = Modifier
            .height(34.dp)
            .clip(RoundedCornerShape(17.dp))
            .background(if (selected) AccentSoft else Color.Transparent)
            .then(if (selected) Modifier else Modifier.border(1.dp, Edge, RoundedCornerShape(17.dp)))
            .clickable(onClick = onClick)
            .padding(horizontal = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (check && selected) {
            Icon(Icons.Outlined.Check, contentDescription = null, tint = fg, modifier = Modifier.size(16.dp))
            Spacer(Modifier.width(6.dp))
        }

        Text(label, style = MaterialTheme.typography.bodyMedium, color = fg, maxLines = 1)
    }
}

enum class PillKind { OK, OFF, PLAIN }

@Composable
fun Pill(text: String, kind: PillKind = PillKind.PLAIN) {
    val (bg, fg) = when (kind) {
        PillKind.OK -> OkSoft to OkText
        PillKind.OFF -> DangerSoft to DangerText
        PillKind.PLAIN -> Surface2 to Ink
    }

    Box(
        modifier = Modifier
            .height(26.dp)
            .clip(RoundedCornerShape(8.dp))
            .background(bg)
            .padding(horizontal = 10.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(text, style = MaterialTheme.typography.labelMedium, color = fg, maxLines = 1)
    }
}

@Composable
fun Segmented(options: List<String>, selected: String, onSelect: (String) -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .height(42.dp)
            .clip(RoundedCornerShape(24.dp))
            .border(1.dp, Edge, RoundedCornerShape(24.dp)),
    ) {
        options.forEach { option ->
            val on = option == selected

            Box(
                modifier = Modifier
                    .weight(1f)
                    .height(42.dp)
                    .background(if (on) AccentSoft else Color.Transparent)
                    .clickable { onSelect(option) },
                contentAlignment = Alignment.Center,
            ) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    if (on) {
                        Icon(Icons.Outlined.Check, contentDescription = null, tint = OnAccentSoft, modifier = Modifier.size(16.dp))
                        Spacer(Modifier.width(6.dp))
                    }

                    Text(
                        option,
                        style = MaterialTheme.typography.bodyMedium,
                        color = if (on) OnAccentSoft else Ink,
                    )
                }
            }
        }
    }
}

private val avatarInk = listOf(
    Color(0xFF2B4C7E) to Color(0xFF9ECBFF),
    Color(0xFF3A2350) to Color(0xFFD2A8FF),
    Color(0xFF3B3418) to Color(0xFFF2CC60),
    Color(0xFF1E4022) to Color(0xFF56D364),
    Color(0xFF4A2520) to Color(0xFFFF9B8A),
    Color(0xFF1C3A60) to Color(0xFF9ECBFF),
)

/** avatarColor is stable per name: a person keeps the same colour between screens. */
fun avatarColor(name: String): Pair<Color, Color> {
    if (name.isEmpty()) {
        return avatarInk[0]
    }

    return avatarInk[Math.floorMod(name.hashCode(), avatarInk.size)]
}

@Composable
fun Avatar(name: String, modifier: Modifier = Modifier, size: Dp = 52.dp, letter: String? = null) {
    val (bg, fg) = avatarColor(name)

    Box(
        modifier = modifier.size(size).clip(CircleShape).background(bg),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            letter ?: name.take(1).uppercase(),
            style = MaterialTheme.typography.titleLarge.copy(fontSize = (size.value * 0.38f).sp),
            color = fg,
        )
    }
}

@Composable
fun SurfaceCard(modifier: Modifier = Modifier, content: @Composable ColumnScope.() -> Unit) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(16.dp))
            .background(Surface1),
        content = content,
    )
}

@Composable
fun SectionLabel(title: String, trailing: String? = null) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 18.dp, bottom = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(title, style = MaterialTheme.typography.titleMedium, color = Ink)

        if (!trailing.isNullOrEmpty()) {
            Spacer(Modifier.weight(1f))
            Text(trailing, style = MaterialTheme.typography.bodySmall, color = Muted, maxLines = 1)
        }
    }
}

@Composable
fun StatCard(
    label: String,
    value: String,
    hint: String? = null,
    valueColor: Color = Ink,
    mono: Boolean = false,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .clip(RoundedCornerShape(14.dp))
            .background(Surface1)
            .padding(14.dp),
    ) {
        Text(label, style = MaterialTheme.typography.labelMedium, color = Muted)
        Text(
            value,
            style = if (mono) monoStyle(MaterialTheme.typography.titleLarge) else MaterialTheme.typography.titleLarge,
            color = valueColor,
            maxLines = 1,
            modifier = Modifier.padding(top = 6.dp),
        )

        if (!hint.isNullOrEmpty()) {
            Text(hint, style = MaterialTheme.typography.labelMedium, color = Muted, modifier = Modifier.padding(top = 2.dp))
        }
    }
}

@Composable
fun StatusDot(color: Color, size: Dp = 10.dp) {
    Box(modifier = Modifier.size(size).clip(CircleShape).background(color))
}

data class NavItem(val label: String, val icon: ImageVector)

@Composable
fun BottomNav(items: List<NavItem>, selected: Int, onSelect: (Int) -> Unit) {
    Column(modifier = Modifier.fillMaxWidth().background(Canvas)) {
        HorizontalDivider(color = Edge)

        Row(
            modifier = Modifier.fillMaxWidth().height(76.dp),
            horizontalArrangement = Arrangement.SpaceEvenly,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            items.forEachIndexed { index, item ->
                val on = index == selected

                Column(
                    modifier = Modifier
                        .width(72.dp)
                        .clip(RoundedCornerShape(12.dp))
                        .clickable { onSelect(index) }
                        .padding(vertical = 4.dp),
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) {
                    Box(
                        modifier = Modifier
                            .width(40.dp)
                            .height(26.dp)
                            .clip(RoundedCornerShape(13.dp))
                            .background(if (on) AccentSoft else Color.Transparent),
                        contentAlignment = Alignment.Center,
                    ) {
                        Icon(
                            item.icon,
                            contentDescription = item.label,
                            tint = if (on) OnAccentSoft else Muted,
                            modifier = Modifier.size(21.dp),
                        )
                    }

                    Text(
                        item.label,
                        style = MaterialTheme.typography.labelSmall,
                        color = if (on) OnAccentSoft else Muted,
                        modifier = Modifier.padding(top = 3.dp),
                    )
                }
            }
        }
    }
}

@Composable
fun RoundFab(onClick: () -> Unit, icon: ImageVector, description: String) {
    Box(
        modifier = Modifier
            .size(56.dp)
            .clip(RoundedCornerShape(18.dp))
            .background(Accent)
            .clickable(onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Icon(icon, contentDescription = description, tint = Color.White, modifier = Modifier.size(26.dp))
    }
}

@Composable
fun ActionPill(onClick: () -> Unit, icon: ImageVector, text: String) {
    Row(
        modifier = Modifier
            .height(56.dp)
            .clip(RoundedCornerShape(18.dp))
            .background(Accent)
            .clickable(onClick = onClick)
            .padding(horizontal = 20.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(icon, contentDescription = null, tint = Color.White, modifier = Modifier.size(22.dp))
        Spacer(Modifier.width(10.dp))
        Text(text, style = MaterialTheme.typography.titleSmall, color = Color.White)
    }
}

@Composable
fun PrimaryButton(text: String, onClick: () -> Unit, enabled: Boolean = true, modifier: Modifier = Modifier) {
    Button(
        onClick = onClick,
        enabled = enabled,
        shape = RoundedCornerShape(23.dp),
        colors = ButtonDefaults.buttonColors(containerColor = Accent, contentColor = Color.White),
        contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = 20.dp, vertical = 10.dp),
        modifier = modifier.height(46.dp),
    ) {
        Text(text, style = MaterialTheme.typography.bodyMedium)
    }
}

@Composable
fun OutlineButton(text: String, onClick: () -> Unit, enabled: Boolean = true, modifier: Modifier = Modifier) {
    androidx.compose.material3.OutlinedButton(
        onClick = onClick,
        enabled = enabled,
        shape = RoundedCornerShape(23.dp),
        border = BorderStroke(1.dp, Edge),
        colors = ButtonDefaults.outlinedButtonColors(contentColor = Ink),
        contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = 20.dp, vertical = 10.dp),
        modifier = modifier.height(46.dp),
    ) {
        Text(text, style = MaterialTheme.typography.bodyMedium)
    }
}

/** AppDialog is the mockup's sheet: dark surface, pill buttons, red when it deletes. */
@Composable
fun AppDialog(
    title: String,
    onDismiss: () -> Unit,
    confirmLabel: String,
    onConfirm: () -> Unit,
    confirmEnabled: Boolean = true,
    dismissLabel: String = "Cancel",
    destructive: Boolean = false,
    content: @Composable ColumnScope.() -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        shape = RoundedCornerShape(24.dp),
        containerColor = Surface2,
        titleContentColor = Ink,
        textContentColor = Ink,
        title = { Text(title, style = MaterialTheme.typography.headlineMedium.copy(fontSize = 20.sp)) },
        text = { Column(content = content) },
        confirmButton = {
            Button(
                onClick = onConfirm,
                enabled = confirmEnabled,
                shape = RoundedCornerShape(23.dp),
                colors = ButtonDefaults.buttonColors(
                    containerColor = if (destructive) Danger else Accent,
                    contentColor = if (destructive) Color.White else Color.White,
                ),
            ) {
                Text(confirmLabel, style = MaterialTheme.typography.bodyMedium)
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) {
                Text(dismissLabel, style = MaterialTheme.typography.bodyMedium, color = Link)
            }
        },
    )
}

/** MonoText is for hosts, commits, dates, IPs and anything transcribed by hand. */
@Composable
fun MonoText(
    text: String,
    color: Color = Ink,
    style: TextStyle = MaterialTheme.typography.bodySmall,
    maxLines: Int = 1,
    modifier: Modifier = Modifier,
) {
    Text(
        text,
        style = monoStyle(style),
        color = color,
        maxLines = maxLines,
        overflow = TextOverflow.Ellipsis,
        modifier = modifier,
    )
}

/** appFieldColors keeps text fields on the palette instead of Material purple. */
@Composable
fun appFieldColors() = androidx.compose.material3.OutlinedTextFieldDefaults.colors(
    focusedTextColor = Ink,
    unfocusedTextColor = Ink,
    focusedContainerColor = Surface1,
    unfocusedContainerColor = Surface1,
    focusedBorderColor = Accent,
    unfocusedBorderColor = Edge,
    focusedLabelColor = Link,
    unfocusedLabelColor = Muted,
    cursorColor = Accent,
)

/** shareText hands a link to whatever app the operator uses to send it. */
fun shareText(ctx: android.content.Context, subject: String, text: String) {
    val send = android.content.Intent(android.content.Intent.ACTION_SEND).apply {
        type = "text/plain"
        putExtra(android.content.Intent.EXTRA_SUBJECT, subject)
        putExtra(android.content.Intent.EXTRA_TEXT, text)
    }

    ctx.startActivity(
        android.content.Intent.createChooser(send, "Share link").apply {
            addFlags(android.content.Intent.FLAG_ACTIVITY_NEW_TASK)
        },
    )
}

/** InfoChip is the outlined chip the mockups use for state that is not a filter. */
@Composable
fun InfoChip(text: String, color: Color = Ink) {
    Box(
        modifier = Modifier
            .height(34.dp)
            .clip(RoundedCornerShape(17.dp))
            .border(1.dp, Edge, RoundedCornerShape(17.dp))
            .padding(horizontal = 14.dp),
        contentAlignment = Alignment.Center,
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(text, style = MaterialTheme.typography.bodyMedium, color = color, maxLines = 1)
        }
    }
}

/** Badge is the action pill on a routing rule: direct, block, or a link. */
@Composable
fun Badge(text: String, background: Color, foreground: Color) {
    Box(
        modifier = Modifier
            .height(30.dp)
            .clip(RoundedCornerShape(10.dp))
            .background(background)
            .padding(horizontal = 12.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(text, style = MaterialTheme.typography.labelMedium, color = foreground, maxLines = 1)
    }
}

/** UnderlineTabs is the mockup's underlined switch: For all, split, full. */
@Composable
fun UnderlineTabs(labels: List<String>, selected: Int, onSelect: (Int) -> Unit) {
    Column {
        Row(modifier = Modifier.fillMaxWidth()) {
            labels.forEachIndexed { index, label ->
                val on = index == selected

                Column(
                    modifier = Modifier.weight(1f).clickable { onSelect(index) },
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) {
                    Text(
                        label,
                        style = MaterialTheme.typography.bodyMedium,
                        color = if (on) Link else Muted,
                        modifier = Modifier.padding(vertical = 12.dp),
                    )
                    Box(
                        modifier = Modifier
                            .fillMaxWidth()
                            .height(2.dp)
                            .background(if (on) Accent else Color.Transparent),
                    )
                }
            }
        }
        HorizontalDivider(color = Edge)
    }
}

/** dashedBorder marks what is not ready: a node the state describes but the
 * host does not answer for yet. */
fun Modifier.dashedBorder(color: Color, radius: Dp = 12.dp, width: Dp = 1.dp): Modifier = drawBehind {
    val stroke = androidx.compose.ui.graphics.drawscope.Stroke(
        width = width.toPx(),
        pathEffect = PathEffect.dashPathEffect(floatArrayOf(10f, 7f)),
    )

    drawRoundRect(
        color = color,
        cornerRadius = androidx.compose.ui.geometry.CornerRadius(radius.toPx()),
        style = stroke,
    )
}
