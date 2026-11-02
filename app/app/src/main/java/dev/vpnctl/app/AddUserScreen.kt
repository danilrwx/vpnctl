package dev.vpnctl.app

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

/**
 * AddUserScreen creates a user with fresh secrets. The core writes, validates and
 * commits; the phone picks the name, the mode and the protocols. The hosts get
 * the account on the next apply, not now — the footer says so.
 */
@Composable
fun AddUserScreen(ctx: android.content.Context, modifier: Modifier, onBack: () -> Unit, onCreated: () -> Unit) {
    val app = remember { Core.current() }
    val served = remember { servedProtocols(app) }
    val scope = rememberCoroutineScope()

    var name by remember { mutableStateOf("") }
    var mode by remember { mutableStateOf("split") }
    var protos by remember { mutableStateOf(defaultProtocols(served)) }
    var tg by remember { mutableStateOf(false) }
    var status by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }

    fun create() {
        if (busy || name.isBlank() || protos.isEmpty()) {
            return
        }

        busy = true
        status = "…"

        scope.launch {
            val out = onIo { addUser(app, name.trim(), setOf(mode), protos, tg) }

            busy = false

            if (out.startsWith("ok")) {
                onCreated()
            } else {
                status = out
            }
        }
    }

    Column(modifier = modifier.fillMaxSize()) {
        ScreenHeader(title = "New user", subtitle = "the hosts get it on the next apply", onBack = onBack)

        LazyColumn(modifier = Modifier.weight(1f)) {
            item {
                SurfaceCard(modifier = Modifier.padding(16.dp)) {
                    Column(modifier = Modifier.fillMaxWidth().padding(16.dp)) {
                        Text("Name", style = MaterialTheme.typography.titleSmall)
                        OutlinedTextField(
                            value = name,
                            onValueChange = { name = it },
                            label = { Text("brat, olga, mama…") },
                            singleLine = true,
                            textStyle = MaterialTheme.typography.bodyMedium,
                            colors = appFieldColors(),
                            modifier = Modifier.fillMaxWidth().padding(top = 8.dp),
                        )
                    }
                }
            }

            item {
                SectionLabel("Mode")
                Segmented(listOf("split", "full"), mode) { mode = it }
                Text(
                    "split routes by rule sets, full sends everything through the exit",
                    style = MaterialTheme.typography.labelMedium,
                    color = Muted,
                    modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                )
            }

            item {
                SectionLabel("Protocols · what this entry serves")
                Row(
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    userProtocols(served).forEach { proto ->
                        Chip(
                            proto,
                            onClick = {
                                protos = if (proto in protos) protos - proto else protos + proto
                            },
                            selected = proto in protos,
                        )
                    }
                }
            }

            item {
                SectionLabel("Telegram")

                SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp)) {
                    Row(
                        modifier = Modifier.fillMaxWidth().padding(16.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Column(modifier = Modifier.weight(1f)) {
                            Text("Telegram proxy", style = MaterialTheme.typography.titleSmall)
                            Text(
                                "adds an mtproto account to the entry",
                                style = MaterialTheme.typography.bodySmall,
                                color = Muted,
                            )
                        }

                        Switch(
                            checked = tg,
                            onCheckedChange = { tg = it },
                            colors = SwitchDefaults.colors(
                                checkedThumbColor = Color.White,
                                checkedTrackColor = Accent,
                                uncheckedThumbColor = Muted,
                                uncheckedTrackColor = Surface2,
                                uncheckedBorderColor = Edge,
                            ),
                        )
                    }
                }
            }

            item {
                if (status.isNotEmpty()) {
                    Text(
                        status,
                        style = MaterialTheme.typography.bodySmall,
                        color = if (status.startsWith("error")) DangerText else Muted,
                        modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                    )
                }
            }

            item { Spacer(Modifier.height(24.dp)) }
        }

        Column(modifier = Modifier.fillMaxWidth().padding(16.dp)) {
            PrimaryButton(
                text = if (busy) "creating…" else "Create user",
                onClick = { create() },
                enabled = !busy && name.isNotBlank() && protos.isNotEmpty(),
                modifier = Modifier.fillMaxWidth(),
            )
        }
    }
}
