package dev.vpnctl.app

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CloudUpload
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Link
import androidx.compose.material.icons.outlined.QrCode2
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * UserScreen is the card from the mockup: who the person is, the subscription as
 * a QR, the direct links, the access they are allowed, and the two actions that
 * end their access. Traffic and "added" are not in the core, so the footer names
 * the commit the card shows (DESIGN.md corrections 1 and 5).
 */
@Composable
fun UserScreen(
    ctx: android.content.Context,
    modifier: Modifier,
    name: String,
    onBack: () -> Unit,
    onDeleted: () -> Unit,
) {
    val app = remember { Core.current() }
    val served = remember { servedProtocols(app) }
    val scope = rememberCoroutineScope()

    var status by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var links by remember { mutableStateOf(emptyList<UserLink>()) }
    var message by remember { mutableStateOf("") }
    var qr by remember { mutableStateOf("") }
    var mode by remember { mutableStateOf(setOf("split")) }
    var protos by remember { mutableStateOf(emptySet<String>()) }
    var tg by remember { mutableStateOf(false) }
    var expires by remember { mutableStateOf("") }
    var enabled by remember { mutableStateOf(true) }
    var lang by remember { mutableStateOf("ru") }
    var deleting by remember { mutableStateOf(false) }
    var typed by remember { mutableStateOf("") }
    var reload by remember { mutableStateOf(0) }

    val head = remember(reload) { app?.head().orEmpty() }

    // One action at a time: two saves over the same file would race, and a
    // disabled control is the only feedback a git commit has.
    fun act(block: suspend () -> String) {
        if (busy) {
            return
        }

        busy = true
        status = "…"

        scope.launch {
            status = onIo(block)
            busy = false
        }
    }

    /** persist writes the whole form: the core replaces modes, protocols and tg. */
    fun persist(
        nextMode: Set<String> = mode,
        nextProtos: Set<String> = protos,
        nextTg: Boolean = tg,
        nextExpires: String = expires,
    ) {
        if (busy) {
            return
        }

        if (nextProtos.isEmpty()) {
            status = "error: choose at least one protocol"
            return
        }

        act {
            val out = saveUser(app, name, nextMode, nextProtos, nextTg, nextExpires)
            if (out.startsWith("ok")) {
                reload++
            }

            out
        }
    }

    LaunchedEffect(name, reload) {
        links = withContext(Dispatchers.IO) {
            app?.let { parseLinks(it.links(name)) } ?: emptyList()
        }

        val row = withContext(Dispatchers.IO) {
            app?.let { parseUsers(it.users()).firstOrNull { u -> u.name == name } }
        }

        if (row != null) {
            mode = row.modes.split(",", "/").map { it.trim() }.filter { it.isNotEmpty() }.toSet()
            protos = row.protocols.toSet()
            tg = row.tg
            expires = row.expires
            enabled = row.enabled
        }
    }

    val expiry = expiryOf(expires)
    val (expiryText, expiryTone) = expiryState(expiry, enabled)

    Column(modifier = modifier.fillMaxSize()) {
        ScreenHeader(
            title = name,
            onBack = onBack,
            actions = {
                IconAction(
                    Icons.Outlined.CloudUpload,
                    { act { pushState(app) } },
                    "Push",
                    tint = if (head.isEmpty()) Muted else Link,
                )
            },
        )

        LazyColumn(modifier = Modifier.weight(1f)) {
            item {
                Row(
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Avatar(name, size = 64.dp)
                    Spacer(Modifier.width(14.dp))

                    Column {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Text(
                                mode.joinToString(" · "),
                                style = MaterialTheme.typography.titleMedium,
                                maxLines = 1,
                            )
                            Spacer(Modifier.width(8.dp))
                            Pill(if (enabled) "active" else "off", if (enabled) PillKind.OK else PillKind.OFF)
                        }

                        Text(
                            listOfNotNull(
                                protos.joinToString(", ").ifEmpty { null },
                                if (tg) "telegram" else null,
                            ).joinToString(" · "),
                            style = MaterialTheme.typography.bodySmall,
                            color = Muted,
                            modifier = Modifier.padding(top = 4.dp),
                        )

                        Text(
                            when {
                                expiry == null -> "no expiry"
                                expiryText == "expired" -> "expired ${expiry.short}"
                                else -> "until ${expiry.short} · $expiryText"
                            },
                            style = MaterialTheme.typography.bodySmall,
                            color = when (expiryTone) {
                                ExpiryTone.Warn -> Warn
                                ExpiryTone.Bad -> DangerText
                                else -> Muted
                            },
                            modifier = Modifier.padding(top = 2.dp),
                        )
                    }
                }
            }

            item {
                Segmented(
                    options = listOf("split", "full"),
                    selected = mode.firstOrNull() ?: "split",
                    onSelect = {
                        if (!busy && mode.firstOrNull() != it) {
                            mode = setOf(it)
                            persist(nextMode = setOf(it))
                        }
                    },
                )
            }

            val sub = links.firstOrNull { it.label.startsWith("subscription") }

            if (sub != null) {
                item {
                    SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp, vertical = 12.dp)) {
                        Column(
                            modifier = Modifier.fillMaxWidth().padding(16.dp),
                            horizontalAlignment = Alignment.CenterHorizontally,
                        ) {
                            Box(
                                modifier = Modifier
                                    .clip(RoundedCornerShape(14.dp))
                                    .background(Color.White)
                                    .padding(12.dp),
                            ) {
                                QrCode(sub.url, 200.dp)
                            }

                            Text(
                                sub.url,
                                style = MaterialTheme.typography.bodySmall,
                                color = Link,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                                modifier = Modifier.padding(top = 14.dp),
                            )
                            Text(
                                "Karing · Hiddify · one URL for both modes",
                                style = MaterialTheme.typography.labelMedium,
                                color = Muted,
                                modifier = Modifier.padding(top = 4.dp),
                            )

                            Row(
                                modifier = Modifier.fillMaxWidth().padding(top = 16.dp),
                                horizontalArrangement = Arrangement.spacedBy(10.dp),
                            ) {
                                OutlineButton(
                                    "Copy link",
                                    onClick = {
                                        copyToClipboard(ctx, sub.url)
                                        status = "subscription link copied"
                                    },
                                    modifier = Modifier.weight(1f),
                                )
                                PrimaryButton(
                                    "Share",
                                    onClick = { shareText(ctx, "subscription", sub.url) },
                                    modifier = Modifier.weight(1f),
                                )
                            }
                        }
                    }
                }
            }

            val direct = links.filterNot { it.label.startsWith("subscription") }

            if (direct.isNotEmpty()) {
                item {
                    SectionLabel("Links", "${direct.size}")

                    SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp)) {
                        direct.forEachIndexed { index, link ->
                            if (index > 0) {
                                androidx.compose.material3.HorizontalDivider(color = Edge)
                            }

                            LinkRow(
                                link = link,
                                onCopy = {
                                    copyToClipboard(ctx, link.url)
                                    status = "${linkTitle(link)} copied"
                                },
                                onQr = { qr = link.url },
                            )
                        }
                    }
                }
            }

            item {
                SectionLabel("Access")

                SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp)) {
                    Column(modifier = Modifier.fillMaxWidth().padding(16.dp)) {
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            userProtocols(served).forEach { proto ->
                                Chip(
                                    proto,
                                    onClick = {
                                        val next = if (proto in protos) protos - proto else protos + proto
                                        if (busy) {
                                            return@Chip
                                        }

                                        if (next.isEmpty()) {
                                            status = "error: choose at least one protocol"
                                        } else {
                                            protos = next
                                            persist(nextProtos = next)
                                        }
                                    },
                                    selected = proto in protos,
                                )
                            }
                        }

                        Row(
                            modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Column(modifier = Modifier.weight(1f)) {
                                Text("Telegram proxy", style = MaterialTheme.typography.titleSmall)
                                Text(
                                    if (tg) "mtproto link is in Links" else "no mtproto account",
                                    style = MaterialTheme.typography.bodySmall,
                                    color = Muted,
                                )
                            }

                            Switch(
                                checked = tg,
                                enabled = !busy,
                                onCheckedChange = {
                                    tg = it
                                    persist(nextTg = it)
                                },
                                colors = SwitchDefaults.colors(
                                    checkedThumbColor = Color.White,
                                    checkedTrackColor = Accent,
                                    uncheckedThumbColor = Muted,
                                    uncheckedTrackColor = Surface2,
                                    uncheckedBorderColor = Edge,
                                ),
                            )
                        }

                        Row(
                            modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            OutlinedTextField(
                                value = expires,
                                onValueChange = { expires = it },
                                label = { Text("Expiry YYYY-MM-DD, empty — none") },
                                singleLine = true,
                                textStyle = MaterialTheme.typography.bodyMedium,
                                colors = appFieldColors(),
                                modifier = Modifier.weight(1f),
                            )
                        }

                        Row(
                            modifier = Modifier.fillMaxWidth().padding(top = 8.dp),
                            horizontalArrangement = Arrangement.spacedBy(8.dp),
                        ) {
                            OutlineButton(
                                "Save expiry",
                                onClick = { persist() },
                                enabled = !busy,
                            )
                            OutlineButton(
                                "Renew 30 d",
                                onClick = {
                                    act {
                                        val out = renewUser(app, name)
                                        if (out.startsWith("ok")) {
                                            reload++
                                        }

                                        out
                                    }
                                },
                                enabled = !busy,
                            )
                        }
                    }
                }
            }

            item {
                SectionLabel("Message for the user")

                SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp)) {
                    Column(modifier = Modifier.fillMaxWidth().padding(16.dp)) {
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            Chip("ru", { lang = "ru" }, selected = lang == "ru")
                            Chip("en", { lang = "en" }, selected = lang == "en")
                            Spacer(Modifier.weight(1f))
                            TextButton(
                                onClick = {
                                    act {
                                        val text = app?.message(name, lang, false) ?: return@act "no state"
                                        message = text
                                        copyToClipboard(ctx, text)
                                        "message copied"
                                    }
                                },
                                enabled = !busy,
                            ) { Text("Copy", color = Link) }
                        }

                        if (message.isNotEmpty()) {
                            Text(
                                message,
                                style = MaterialTheme.typography.bodySmall,
                                color = Muted,
                                modifier = Modifier.padding(top = 8.dp),
                            )
                        }
                    }
                }
            }

            item { Spacer(Modifier.height(24.dp)) }
        }

        if (status.isNotEmpty()) {
            Text(
                status,
                style = MaterialTheme.typography.bodySmall,
                color = when {
                    status.startsWith("error") -> DangerText
                    status.startsWith("ok") -> OkText
                    else -> Muted
                },
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
            )
        }

        Row(
            modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            OutlineButton(
                if (enabled) "Disable" else "Enable",
                onClick = {
                    act {
                        val out = setUserEnabled(app, name, !enabled)
                        if (out.startsWith("ok")) {
                            reload++
                        }

                        out
                    }
                },
                enabled = !busy,
            )

            TextButton(
                onClick = {
                    typed = ""
                    deleting = true
                },
                enabled = !busy,
            ) { Text("Delete", color = DangerText) }

            Spacer(Modifier.weight(1f))

            MonoText(head.ifEmpty { "—" }, color = Muted, style = MaterialTheme.typography.labelMedium)
        }
    }

    if (qr.isNotEmpty()) {
        QrDialog(links.firstOrNull { it.url == qr }?.let { linkTitle(it) } ?: "QR", qr, onDismiss = { qr = "" })
    }

    if (deleting) {
        AppDialog(
            title = "Delete user $name?",
            onDismiss = { deleting = false },
            confirmLabel = "Delete",
            destructive = true,
            confirmEnabled = !busy && typed.trim() == name,
            onConfirm = {
                deleting = false

                act {
                    val out = removeUser(app, name)
                    if (out.startsWith("ok")) {
                        onDeleted()
                    }

                    out
                }
            },
            content = {
                Text("After the next apply:", style = MaterialTheme.typography.bodySmall, color = Muted)
                Text(
                    "· ${mode.size * protos.size} engine accounts and ${mode.size} subscription${if (mode.size == 1) "" else "s"} go away",
                    style = MaterialTheme.typography.bodySmall,
                    modifier = Modifier.padding(top = 4.dp),
                )
                Text(
                    "· secrets leave secrets.age.yaml; the UUID stays in git history",
                    style = MaterialTheme.typography.bodySmall,
                    modifier = Modifier.padding(top = 4.dp),
                )
                Text(
                    "Type the name to confirm",
                    style = MaterialTheme.typography.labelMedium,
                    color = Muted,
                    modifier = Modifier.padding(top = 12.dp, bottom = 6.dp),
                )
                OutlinedTextField(
                    value = typed,
                    onValueChange = { typed = it },
                    singleLine = true,
                    textStyle = MaterialTheme.typography.bodyMedium,
                    colors = appFieldColors(),
                    modifier = Modifier.fillMaxWidth(),
                )
            },
        )
    }
}

/** linkTitle names a link by what it is, not by the position the core gave it. */
fun linkTitle(link: UserLink): String = when {
    link.url.startsWith("tg://") -> "Telegram proxy"
    link.url.startsWith("hysteria2") -> "hy2"
    link.url.contains("type=ws") -> "ws"
    link.url.startsWith("vless://") -> "reality"
    else -> link.label.replaceFirstChar { it.uppercase() }
}

@Composable
fun LinkRow(link: UserLink, onCopy: () -> Unit, onQr: () -> Unit) {
    Row(
        modifier = Modifier.fillMaxWidth().clickable(onClick = onCopy).padding(horizontal = 16.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            modifier = Modifier.size(36.dp).clip(CircleShape).background(Surface2),
            contentAlignment = Alignment.Center,
        ) {
            Icon(Icons.Outlined.Link, contentDescription = null, tint = Muted, modifier = Modifier.size(18.dp))
        }

        Column(modifier = Modifier.weight(1f).padding(start = 12.dp)) {
            Text(linkTitle(link), style = MaterialTheme.typography.titleSmall, maxLines = 1)
            MonoText(
                link.url,
                color = Muted,
                style = MaterialTheme.typography.labelMedium,
                modifier = Modifier.padding(top = 2.dp),
            )
        }

        TextButton(onClick = onCopy) {
            Icon(Icons.Outlined.ContentCopy, contentDescription = null, tint = Link, modifier = Modifier.size(16.dp))
            Spacer(Modifier.width(4.dp))
            Text("Copy", color = Link, style = MaterialTheme.typography.bodySmall)
        }
        IconAction(Icons.Outlined.QrCode2, onQr, "QR", tint = Link)
    }
}
