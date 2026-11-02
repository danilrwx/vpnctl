package dev.vpnctl.app

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
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
import androidx.compose.material.icons.outlined.LaptopMac
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.material.icons.outlined.Smartphone
import androidx.compose.material.icons.outlined.VpnKey
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
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
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import mobile.Mobile
import java.io.File

/**
 * DevicesScreen is who can read the secrets and what this phone holds: the age
 * recipient, the ssh key the hosts know, and the backup that survives a wipe.
 * The repository lives here too, because a fresh install has no other screen to
 * say where the state comes from.
 */
@Composable
fun DevicesScreen(ctx: android.content.Context, modifier: Modifier, onBack: () -> Unit) {
    var devices by remember { mutableStateOf(emptyList<DeviceRow>()) }
    var repo by remember { mutableStateOf(Prefs.repo(ctx).ifEmpty { REPO }) }
    var status by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    // the core is reopened after a restore, and the recipient file changes with
    // it: this counter is what makes the screen read both again
    var version by remember { mutableStateOf(0) }

    var exporting by remember { mutableStateOf(false) }
    var exportPass by remember { mutableStateOf("") }
    var exportErr by remember { mutableStateOf("") }

    var restoring by remember { mutableStateOf(false) }
    var restoreBlob by remember { mutableStateOf("") }
    var restorePass by remember { mutableStateOf("") }
    var restoreErr by remember { mutableStateOf("") }

    var revoking by remember { mutableStateOf<DeviceRow?>(null) }
    var revokeTyped by remember { mutableStateOf("") }

    var addingDevice by remember { mutableStateOf(false) }
    var deviceName by remember { mutableStateOf("") }
    var deviceKey by remember { mutableStateOf("") }
    var addDeviceErr by remember { mutableStateOf("") }

    var replacing by remember { mutableStateOf(false) }
    var backupPass by remember { mutableStateOf("") }
    var backupErr by remember { mutableStateOf("") }
    var backupBlob by remember { mutableStateOf("") }

    var alerts by remember { mutableStateOf(AlertsRow()) }
    var hostRows by remember { mutableStateOf(emptyList<HostRow>()) }
    var settingAlerts by remember { mutableStateOf(false) }
    var alertsEntry by remember { mutableStateOf("") }
    var alertsTopic by remember { mutableStateOf("") }

    val scope = rememberCoroutineScope()
    val recipient = remember(version) { localRecipient(ctx) }
    val sshKey = remember { File(ctx.filesDir, "identity.pub").takeIf { it.exists() }?.readText()?.trim().orEmpty() }

    LaunchedEffect(version) {
        devices = withContext(Dispatchers.IO) {
            Core.current()?.let { parseDevices(it.devices()) } ?: emptyList()
        }
        alerts = withContext(Dispatchers.IO) {
            Core.current()?.let { parseAlerts(it.alerts()) } ?: AlertsRow()
        }
        hostRows = withContext(Dispatchers.IO) {
            Core.current()?.let { parseHosts(it.hosts()) } ?: emptyList()
        }
    }

    fun connect() {
        val url = normalizeRepo(repo)
        if (busy || url.isBlank()) {
            return
        }

        repo = url
        busy = true
        status = "…"

        scope.launch {
            Prefs.setRepo(ctx, url)
            status = onIo { Core.connect(ctx, url) }
            version++
            busy = false
        }
    }

    val others = devices.filter { it.recipient != recipient }

    Column(modifier = modifier.fillMaxSize()) {
        ScreenHeader(
            title = "Devices and keys",
            subtitle = "age.yaml · ${devices.size} ${plural(devices.size, "recipient", "recipients")}",
            onBack = onBack,
            stacked = true,
        )

        LazyColumn(modifier = Modifier.weight(1f)) {
            item {
                ThisDeviceCard(
                    recipient = recipient,
                    sshKey = sshKey,
                    onCopyRecipient = {
                        copyToClipboard(ctx, recipient)
                        status = "recipient copied"
                    },
                    onCopyKey = {
                        copyToClipboard(ctx, sshKey)
                        status = "ssh key copied"
                    },
                    onExport = {
                        exportPass = ""
                        exportErr = ""
                        exporting = true
                    },
                    onRestore = {
                        restoreBlob = ""
                        restorePass = ""
                        restoreErr = ""
                        restoring = true
                    },
                    onAddDevice = {
                        deviceName = ""
                        deviceKey = ""
                        addDeviceErr = ""
                        addingDevice = true
                    },
                )
            }

            if (others.isNotEmpty()) {
                item { SectionLabel("Recipients", "${others.size}") }

                item {
                    SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp)) {
                        others.forEachIndexed { index, device ->
                            if (index > 0) {
                                HorizontalDivider(color = Edge)
                            }

                            RecipientRow(
                                device = device,
                                onRevoke = {
                                    revokeTyped = ""
                                    revoking = device
                                },
                                onReplace = {
                                    backupPass = ""
                                    backupErr = ""
                                    replacing = true
                                },
                                enabled = !busy,
                            )
                        }
                    }
                }
            }

            item {
                SectionLabel("Notifications")

                SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp)) {
                    Column(modifier = Modifier.fillMaxWidth().padding(16.dp)) {
                        if (alerts.url.isEmpty()) {
                            Text("off", style = MaterialTheme.typography.titleSmall)
                            Text(
                                "the entry can notify the phone when the chain probe fails",
                                style = MaterialTheme.typography.bodySmall,
                                color = Muted,
                            )
                        } else {
                            MonoText(alerts.url, color = Link, style = MaterialTheme.typography.bodySmall, maxLines = 2)
                            Text(
                                "served by ${alerts.serve.ifEmpty { "another host" }} · ${if (alerts.token) "token in secrets" else "no token"}",
                                style = MaterialTheme.typography.labelMedium,
                                color = Muted,
                                modifier = Modifier.padding(top = 4.dp),
                            )
                        }

                        Row(modifier = Modifier.padding(top = 10.dp), horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                            TextButton(
                                onClick = {
                                    alertsEntry = alerts.serve.ifEmpty {
                                        hostRows.firstOrNull { it.role == "entry" }?.name.orEmpty()
                                    }
                                    alertsTopic = alerts.url.substringAfterLast('/').ifEmpty { "" }
                                    settingAlerts = true
                                },
                            ) {
                                Text(if (alerts.url.isEmpty()) "Set up" else "Change", color = Link, style = MaterialTheme.typography.bodySmall)
                            }

                            if (alerts.url.isNotEmpty()) {
                                TextButton(onClick = {
                                    busy = true
                                    scope.launch {
                                        status = onIo { setAlertsText(Core.current(), "", "") }
                                        version++
                                        busy = false
                                    }
                                }, enabled = !busy) {
                                    Text("Turn off", color = DangerText, style = MaterialTheme.typography.bodySmall)
                                }
                            }
                        }
                    }
                }
            }

            item {
                SectionLabel("State repository")

                SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp)) {
                    Column(modifier = Modifier.fillMaxWidth().padding(16.dp)) {
                        Text(
                            "the phone clones with its own deploy key; connecting replaces the local copy",
                            style = MaterialTheme.typography.bodySmall,
                            color = Muted,
                        )

                        OutlinedTextField(
                            value = repo,
                            onValueChange = { repo = it },
                            label = { Text("owner/repo or a GitHub URL") },
                            singleLine = true,
                            textStyle = MaterialTheme.typography.bodyMedium,
                            colors = appFieldColors(),
                            modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
                        )

                        Row(
                            modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
                            horizontalArrangement = Arrangement.spacedBy(10.dp),
                        ) {
                            PrimaryButton("Connect", { connect() }, enabled = !busy && repo.isNotBlank())
                            OutlineButton(
                                "Push",
                                onClick = {
                                    busy = true
                                    scope.launch {
                                        status = onIo { pushState(Core.current()) }
                                        busy = false
                                    }
                                },
                                enabled = !busy,
                            )
                        }
                    }
                }
            }

            item {
                SectionLabel("What this means")

                SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp)) {
                    Column(modifier = Modifier.fillMaxWidth().padding(16.dp)) {
                        listOf(
                            "every recipient can read secrets.age.yaml · private keys never leave their device",
                            "revoking re-encrypts for the rest; old commits stay readable by the revoked key",
                            "host access keys live in authorized_keys with a forced command for the agent",
                        ).forEach { line ->
                            Text(
                                "· $line",
                                style = MaterialTheme.typography.bodySmall,
                                color = Muted,
                                modifier = Modifier.padding(bottom = 6.dp),
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
                color = if (status.startsWith("error")) DangerText else Muted,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 6.dp),
            )
        }
    }

    if (exporting) {
        AppDialog(
            title = "Export this identity",
            onDismiss = { exporting = false },
            confirmLabel = "Seal and copy",
            confirmEnabled = !busy && exportPass.length >= 8,
            onConfirm = {
                busy = true

                scope.launch {
                    val (blob, err) = withContext(Dispatchers.IO) { sealIdentityText(exportPass) }

                    if (err.isEmpty()) {
                        copyToClipboard(ctx, blob)
                        status = "sealed and copied · store it off this phone"
                        exporting = false
                    } else {
                        exportErr = err
                    }

                    busy = false
                }
            },
            content = {
                Text(
                    "The text is the age key under this password. Anyone holding both reads every secret, and nothing ties it to this phone.",
                    style = MaterialTheme.typography.bodySmall,
                    color = Muted,
                )
                Text(
                    "At least 8 characters. Store it away from the repository.",
                    style = MaterialTheme.typography.labelMedium,
                    color = Muted,
                    modifier = Modifier.padding(top = 8.dp),
                )
                OutlinedTextField(
                    value = exportPass,
                    onValueChange = { exportPass = it },
                    label = { Text("Password") },
                    singleLine = true,
                    visualTransformation = PasswordVisualTransformation(),
                    textStyle = MaterialTheme.typography.bodyMedium,
                    colors = appFieldColors(),
                    modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
                )

                if (busy) {
                    Text("sealing…", style = MaterialTheme.typography.bodySmall, color = Muted)
                }

                if (exportErr.isNotEmpty()) {
                    Text(exportErr, style = MaterialTheme.typography.bodySmall, color = DangerText)
                }
            },
        )
    }

    if (restoring) {
        AppDialog(
            title = "Restore with a password",
            onDismiss = { restoring = false },
            confirmLabel = "Restore",
            confirmEnabled = !busy && restoreBlob.isNotBlank() && restorePass.isNotEmpty(),
            onConfirm = {
                busy = true

                scope.launch {
                    val (msg, err) = withContext(Dispatchers.IO) {
                        restoreIdentityText(ctx, restoreBlob, restorePass)
                    }

                    if (err.isEmpty()) {
                        status = msg
                        version++
                        restoring = false
                    } else {
                        restoreErr = err
                    }

                    busy = false
                }
            },
            content = {
                Text(
                    "Paste the backup you stored at setup. The identity replaces this phone's own; a key that does not decrypt the secrets here is refused.",
                    style = MaterialTheme.typography.bodySmall,
                    color = Muted,
                )
                OutlinedTextField(
                    value = restoreBlob,
                    onValueChange = { restoreBlob = it },
                    label = { Text("-----BEGIN AGE ENCRYPTED FILE-----") },
                    minLines = 3,
                    maxLines = 6,
                    textStyle = MaterialTheme.typography.labelMedium,
                    colors = appFieldColors(),
                    modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
                )
                OutlinedTextField(
                    value = restorePass,
                    onValueChange = { restorePass = it },
                    label = { Text("Password") },
                    singleLine = true,
                    visualTransformation = PasswordVisualTransformation(),
                    textStyle = MaterialTheme.typography.bodyMedium,
                    colors = appFieldColors(),
                    modifier = Modifier.fillMaxWidth().padding(top = 8.dp),
                )

                if (busy) {
                    Text("unlocking…", style = MaterialTheme.typography.bodySmall, color = Muted)
                }

                if (restoreErr.isNotEmpty()) {
                    Text(restoreErr, style = MaterialTheme.typography.bodySmall, color = DangerText)
                }
            },
        )
    }

    revoking?.let { device ->
        AppDialog(
            title = "Revoke ${device.name}?",
            onDismiss = { revoking = null },
            confirmLabel = "Revoke",
            destructive = true,
            confirmEnabled = !busy && revokeTyped.trim() == device.name,
            onConfirm = {
                busy = true

                scope.launch {
                    status = onIo { revokeDevice(Core.current(), device.name) }
                    version++
                    revoking = null
                    busy = false
                }
            },
            content = {
                listOf(
                    "age.yaml loses this recipient and secrets.age.yaml is re-encrypted for the rest",
                    "this phone keeps its own access",
                    "old commits stay readable with this key: assume it has read them",
                    "its ssh key stays in authorized_keys on the hosts until it is removed there by hand",
                ).forEach { line ->
                    Text(
                        "· $line",
                        style = MaterialTheme.typography.bodySmall,
                        color = Muted,
                        modifier = Modifier.padding(bottom = 4.dp),
                    )
                }

                Text(
                    "Type the name to confirm",
                    style = MaterialTheme.typography.labelMedium,
                    color = Muted,
                    modifier = Modifier.padding(top = 12.dp, bottom = 6.dp),
                )
                OutlinedTextField(
                    value = revokeTyped,
                    onValueChange = { revokeTyped = it },
                    singleLine = true,
                    textStyle = MaterialTheme.typography.bodyMedium,
                    colors = appFieldColors(),
                    modifier = Modifier.fillMaxWidth(),
                )
            },
        )
    }

    if (settingAlerts) {
        AppDialog(
            title = "Notifications",
            onDismiss = { settingAlerts = false },
            confirmLabel = "Save",
            confirmEnabled = !busy && alertsEntry.isNotEmpty() && alertsTopic.isNotBlank(),
            onConfirm = {
                busy = true

                scope.launch {
                    status = onIo { setAlertsText(Core.current(), alertsEntry, alertsTopic.trim()) }
                    version++
                    settingAlerts = false
                    busy = false
                }
            },
            content = {
                Text(
                    "The entry runs ntfy behind its domain and the chain probe posts there. The public name is derived from the entry, so it cannot drift.",
                    style = MaterialTheme.typography.bodySmall,
                    color = Muted,
                )

                Text("entry", style = MaterialTheme.typography.labelMedium, color = Muted, modifier = Modifier.padding(top = 10.dp, bottom = 4.dp))
                hostRows.filter { it.role == "entry" }.forEach { host ->
                    Chip(host.name, { alertsEntry = host.name }, selected = alertsEntry == host.name)
                }

                OutlinedTextField(
                    value = alertsTopic,
                    onValueChange = { alertsTopic = it },
                    label = { Text("topic, a-z 0-9 _ -") },
                    singleLine = true,
                    textStyle = MaterialTheme.typography.bodyMedium,
                    colors = appFieldColors(),
                    modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
                )
            },
        )
    }

    if (addingDevice) {
        AppDialog(
            title = "Add a device",
            onDismiss = { addingDevice = false },
            confirmLabel = "Add",
            confirmEnabled = !busy && deviceName.isNotBlank() && deviceKey.isNotBlank(),
            onConfirm = {
                busy = true

                scope.launch {
                    val out = onIo { addDeviceText(Core.current(), deviceName, deviceKey) }

                    if (out.startsWith("ok")) {
                        status = out
                        version++
                        addingDevice = false
                    } else {
                        addDeviceErr = out
                    }

                    busy = false
                }
            },
            content = {
                Text(
                    "The key is generated on the device that will hold it: open the app there, copy its age recipient, and paste it here. The private half never leaves that device.",
                    style = MaterialTheme.typography.bodySmall,
                    color = Muted,
                )
                OutlinedTextField(
                    value = deviceName,
                    onValueChange = { deviceName = it },
                    label = { Text("name, e.g. tablet") },
                    singleLine = true,
                    textStyle = MaterialTheme.typography.bodyMedium,
                    colors = appFieldColors(),
                    modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
                )
                OutlinedTextField(
                    value = deviceKey,
                    onValueChange = { deviceKey = it },
                    label = { Text("age1…") },
                    minLines = 2,
                    maxLines = 4,
                    textStyle = MaterialTheme.typography.labelMedium,
                    colors = appFieldColors(),
                    modifier = Modifier.fillMaxWidth().padding(top = 8.dp),
                )

                if (addDeviceErr.isNotEmpty()) {
                    Text(addDeviceErr, style = MaterialTheme.typography.bodySmall, color = DangerText)
                }
            },
        )
    }

    if (replacing) {
        AppDialog(
            title = "Replace the offline backup",
            onDismiss = { replacing = false },
            confirmLabel = "Replace",
            confirmEnabled = !busy && backupPass.length >= 8,
            onConfirm = {
                busy = true

                scope.launch {
                    val (blob, err) = withContext(Dispatchers.IO) { newBackupText(Core.current(), backupPass) }

                    if (err.isEmpty()) {
                        backupBlob = blob
                        status = "backup replaced · store the new blob off this phone"
                        replacing = false
                    } else {
                        backupErr = err
                    }

                    busy = false
                }
            },
            content = {
                Text(
                    "A new backup identity replaces the old one in age.yaml, and the secrets are re-encrypted for it. The old blob stops working. At least 8 characters; store the new text away from the repository.",
                    style = MaterialTheme.typography.bodySmall,
                    color = Muted,
                )
                OutlinedTextField(
                    value = backupPass,
                    onValueChange = { backupPass = it },
                    label = { Text("Password") },
                    singleLine = true,
                    visualTransformation = PasswordVisualTransformation(),
                    textStyle = MaterialTheme.typography.bodyMedium,
                    colors = appFieldColors(),
                    modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
                )

                if (backupErr.isNotEmpty()) {
                    Text(backupErr, style = MaterialTheme.typography.bodySmall, color = DangerText)
                }
            },
        )
    }

    if (backupBlob.isNotEmpty()) {
        AppDialog(
            title = "New backup identity",
            onDismiss = { backupBlob = "" },
            confirmLabel = "Copy and close",
            onConfirm = {
                copyToClipboard(ctx, backupBlob)
                backupBlob = ""
                status = "backup copied · store it off this phone"
            },
            dismissLabel = "Close",
            content = {
                Text(
                    "Shown once. Anyone with this text and its password can read every secret; the repository does not contain it.",
                    style = MaterialTheme.typography.bodySmall,
                    color = Muted,
                )
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .height(180.dp)
                        .padding(top = 8.dp)
                        .clip(RoundedCornerShape(12.dp))
                        .background(Canvas)
                        .verticalScroll(rememberScrollState())
                        .padding(12.dp),
                ) {
                    Text(backupBlob, style = monoStyle(MaterialTheme.typography.labelMedium), color = Ink)
                }
            },
        )
    }
}

@Composable
fun ThisDeviceCard(
    recipient: String,
    sshKey: String,
    onCopyRecipient: () -> Unit,
    onCopyKey: () -> Unit,
    onExport: () -> Unit,
    onRestore: () -> Unit,
    onAddDevice: () -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 8.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(Surface1)
            .border(1.dp, Accent, RoundedCornerShape(16.dp))
            .padding(16.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Box(
                modifier = Modifier.size(36.dp).clip(CircleShape).background(AccentSoft),
                contentAlignment = Alignment.Center,
            ) {
                Icon(Icons.Outlined.Smartphone, contentDescription = null, tint = OnAccentSoft, modifier = Modifier.size(20.dp))
            }

            Column(modifier = Modifier.padding(start = 12.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text("this phone", style = MaterialTheme.typography.titleMedium)
                    Spacer(Modifier.width(8.dp))
                    Pill("keystore", PillKind.OK)
                }
                Text(
                    "the age key and the agent key are wrapped by the Keystore",
                    style = MaterialTheme.typography.bodySmall,
                    color = Muted,
                )
            }
        }

        DeviceFact("secrets", shortKey(recipient), "wrapped", Warn)
        DeviceFact("agent", if (sshKey.isEmpty()) "none" else "ecdsa P-256", "keystore", OkText)

        Row(modifier = Modifier.padding(top = 12.dp), horizontalArrangement = Arrangement.spacedBy(4.dp)) {
            TextButton(onClick = onCopyRecipient, enabled = recipient.isNotEmpty()) {
                Text("Copy recipient", color = Link, style = MaterialTheme.typography.bodySmall)
            }
            TextButton(onClick = onCopyKey, enabled = sshKey.isNotEmpty()) {
                Text("Copy key", color = Link, style = MaterialTheme.typography.bodySmall)
            }
        }

        Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
            TextButton(onClick = onExport) {
                Text("Export under password", color = Link, style = MaterialTheme.typography.bodySmall)
            }
            TextButton(onClick = onRestore) {
                Text("Restore or transfer", color = Link, style = MaterialTheme.typography.bodySmall)
            }
        }

        Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
            TextButton(onClick = onAddDevice) {
                Text("Add a device", color = Link, style = MaterialTheme.typography.bodySmall)
            }
        }
    }
}

@Composable
private fun DeviceFact(label: String, value: String, state: String, stateColor: Color) {
    Row(modifier = Modifier.fillMaxWidth().padding(top = 10.dp), verticalAlignment = Alignment.CenterVertically) {
        Text(label, style = MaterialTheme.typography.bodySmall, color = Muted, modifier = Modifier.width(64.dp))
        MonoText(value, color = Ink, style = MaterialTheme.typography.labelMedium, modifier = Modifier.weight(1f))
        Text(state, style = MaterialTheme.typography.labelSmall, color = stateColor)
    }
}

@Composable
private fun RecipientRow(
    device: DeviceRow,
    onRevoke: () -> Unit,
    onReplace: () -> Unit,
    enabled: Boolean,
) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            modifier = Modifier.size(36.dp).clip(CircleShape).background(Surface2),
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                if (device.name == "backup") Icons.Outlined.Lock else Icons.Outlined.LaptopMac,
                contentDescription = null,
                tint = Muted,
                modifier = Modifier.size(18.dp),
            )
        }

        Column(modifier = Modifier.weight(1f).padding(start = 12.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(device.name, style = MaterialTheme.typography.titleSmall, maxLines = 1)
                if (device.added.isNotEmpty()) {
                    Text(
                        "  added ${device.added}",
                        style = MaterialTheme.typography.bodySmall,
                        color = Muted,
                    )
                }
            }

            MonoText(
                shortKey(device.recipient),
                color = Muted,
                style = MaterialTheme.typography.labelMedium,
                modifier = Modifier.padding(top = 2.dp),
            )
        }

        if (device.name == "backup") {
            TextButton(onClick = onReplace, enabled = enabled) {
                Text("Replace", color = Link, style = MaterialTheme.typography.bodySmall)
            }
        } else {
            TextButton(onClick = onRevoke, enabled = enabled) {
                Text("Revoke", color = DangerText, style = MaterialTheme.typography.bodySmall)
            }
        }
    }
}

/** shortKey keeps a key recognisable without filling the row: age1abc…f2k0. */
fun shortKey(key: String): String {
    val clean = key.trim()

    if (clean.length <= 16) {
        return clean
    }

    return "${clean.take(10)}…${clean.takeLast(4)}"
}

/** localRecipient is the age recipient this phone minted on first launch. */
fun localRecipient(ctx: android.content.Context): String =
    File(ctx.filesDir, "age.recipient").takeIf { it.exists() }?.readText()?.trim().orEmpty()

/** sealIdentityText returns the armored blob and an error line, never both. */
fun sealIdentityText(pass: String): Pair<String, String> = try {
    Mobile.sealIdentity(pass) to ""
} catch (e: Exception) {
    "" to "error: ${e.message}"
}

/**
 * restoreIdentityText swaps the phone's key for the one in the blob. The Vault
 * write happens only after the core has proven the key decrypts the state, so a
 * wrong backup cannot leave the app with a key that opens nothing.
 */
fun restoreIdentityText(ctx: android.content.Context, blob: String, pass: String): Pair<String, String> {
    val dir = File(ctx.filesDir, "state").absolutePath

    return try {
        val id = Mobile.unsealIdentity(blob, pass, dir)
        Vault(ctx.filesDir).put("age.key", id.secret)
        File(ctx.filesDir, "age.recipient").writeText(id.recipient)

        // unseal refuses a key that cannot decrypt the local secrets, so getting
        // here means the check passed; say which check, because the state may
        // also not be here yet
        val checked = if (File(dir, "secrets.age.yaml").exists()) " · decrypts secrets.age.yaml" else " · no secrets here to check yet"
        val opened = Core.reopen(ctx)

        if (opened == null) {
            "restored ${id.recipient}$checked, but the state did not open" to ""
        } else {
            "restored ${id.recipient}$checked" to ""
        }
    } catch (e: Exception) {
        "" to "error: ${restoreFailure(e.message.orEmpty())}"
    }
}

/**
 * restoreFailure keeps the age library's error chain out of the dialog: it names
 * the same thing three times ("incorrect passphrase" twice), and what the user
 * has to act on is whether the password or the backup is wrong.
 */
fun restoreFailure(message: String): String = when {
    message.contains("wrong passphrase") -> "wrong password, or the backup is damaged"
    message.contains("does not decrypt") -> "this key does not read the secrets here: it is not a recipient of age.yaml"
    else -> message.substringAfter("error: ").trim()
}

/** AlertsRow is the notification setup the screen shows. */
data class AlertsRow(val url: String = "", val serve: String = "", val token: Boolean = false)

/** parseAlerts reads mobile.Alerts. */
fun parseAlerts(json: String): AlertsRow = try {
    val o = org.json.JSONObject(json)
    AlertsRow(o.optString("url"), o.optString("serve"), o.optBoolean("token"))
} catch (e: Exception) {
    AlertsRow()
}

/** setAlertsText points notifications at an entry, or turns them off. */
fun setAlertsText(app: mobile.App?, serve: String, topic: String): String {
    if (app == null) {
        return "no state"
    }

    return try {
        "ok: ${app.setAlerts(serve, topic)} · push when the forge is reachable"
    } catch (e: Exception) {
        "error: ${e.message}"
    }
}

/** addDeviceText adds a recipient generated on another device. */
fun addDeviceText(app: mobile.App?, name: String, key: String): String {
    if (app == null) {
        return "no state"
    }

    return try {
        "ok: ${app.addDevice(name, key)} · push when the forge is reachable"
    } catch (e: Exception) {
        "error: ${e.message}"
    }
}

/** newBackupText replaces the offline backup and returns the sealed blob. */
fun newBackupText(app: mobile.App?, passphrase: String): Pair<String, String> {
    if (app == null) {
        return "" to "no state"
    }

    return try {
        app.newBackup(passphrase) to ""
    } catch (e: Exception) {
        "" to "error: ${e.message}"
    }
}

/** revokeDevice drops a recipient and re-encrypts the secrets for the rest. */
fun revokeDevice(app: mobile.App?, name: String): String {
    if (app == null) {
        return "no state"
    }

    return try {
        "ok: ${app.removeDevice(name)} · push when the forge is reachable"
    } catch (e: Exception) {
        "error: ${e.message}"
    }
}
