package dev.vpnctl.app

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
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
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.time.LocalDate

/**
 * AddUserScreen creates a user with fresh secrets. The core writes, validates and
 * commits; the phone picks the name, the mode, the protocols and the expiry, and
 * the summary says what those choices produce before anything is written.
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
    var term by remember { mutableStateOf("none") }
    var date by remember { mutableStateOf("") }
    var domain by remember { mutableStateOf("") }
    var status by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }

    LaunchedEffect(Unit) {
        domain = withContext(Dispatchers.IO) {
            app?.let { parseHosts(it.hosts()) }?.firstOrNull { it.role == "entry" }?.domain.orEmpty()
        }
    }

    /** expires turns the choice into the YYYY-MM-DD the core stores. */
    fun expires(): String = when (term) {
        "30 d" -> LocalDate.now().plusDays(30).toString()
        "90 d" -> LocalDate.now().plusDays(90).toString()
        "date…" -> date.trim()
        else -> ""
    }

    fun create() {
        if (busy || name.isBlank() || protos.isEmpty()) {
            return
        }

        busy = true
        status = "…"

        scope.launch {
            val out = onIo { addUser(app, name.trim(), setOf(mode), protos, tg, expires()) }

            busy = false

            if (out.startsWith("ok")) {
                onCreated()
            } else {
                status = out
            }
        }
    }

    val accounts = protos.size

    Column(modifier = modifier.fillMaxSize()) {
        ScreenHeader(title = "New user", subtitle = "the hosts get it on the next apply", onBack = onBack)

        LazyColumn(modifier = Modifier.weight(1f)) {
            item {
                SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) {
                    Column(modifier = Modifier.fillMaxWidth().padding(16.dp)) {
                        Text("Name", style = MaterialTheme.typography.titleSmall)
                        OutlinedTextField(
                            value = name,
                            onValueChange = { name = it },
                            label = { Text("lowercase, digits and dashes") },
                            singleLine = true,
                            textStyle = MaterialTheme.typography.bodyMedium,
                            colors = appFieldColors(),
                            modifier = Modifier.fillMaxWidth().padding(top = 8.dp),
                        )
                        Text(
                            "the name goes into every engine account and subscription link",
                            style = MaterialTheme.typography.labelMedium,
                            color = Muted,
                            modifier = Modifier.padding(top = 6.dp),
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
                Text(
                    "only the inbounds this entry has enabled; anything else fails at save",
                    style = MaterialTheme.typography.labelMedium,
                    color = Muted,
                    modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                )
            }

            item {
                SectionLabel("Expiry")

                SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp)) {
                    Column(modifier = Modifier.fillMaxWidth().padding(16.dp)) {
                        Segmented(listOf("none", "30 d", "90 d", "date…"), term) { term = it }

                        if (term == "date…") {
                            OutlinedTextField(
                                value = date,
                                onValueChange = { date = it },
                                label = { Text("YYYY-MM-DD") },
                                singleLine = true,
                                textStyle = MaterialTheme.typography.bodyMedium,
                                colors = appFieldColors(),
                                modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
                            )
                        }

                        Text(
                            when (term) {
                                "none" -> "no expiry: the account works until it is disabled"
                                "date…" -> "the day the account stops; apply removes it at that date"
                                else -> "expires ${LocalDate.now().plusDays(if (term == "30 d") 30 else 90)}"
                            },
                            style = MaterialTheme.typography.labelMedium,
                            color = Muted,
                            modifier = Modifier.padding(top = 10.dp),
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

            if (name.isNotBlank()) {
                item {
                    SectionLabel("What will be created")

                    SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp)) {
                        Column(modifier = Modifier.fillMaxWidth().padding(16.dp)) {
                            Text(
                                "$accounts ${plural(accounts, "account", "accounts")}",
                                style = MaterialTheme.typography.titleSmall,
                            )
                            MonoText(
                                "${name.trim()}-{$mode}-{${protos.sorted().joinToString(", ")}}",
                                color = Muted,
                                style = MaterialTheme.typography.bodySmall,
                                maxLines = 2,
                                modifier = Modifier.padding(top = 4.dp),
                            )
                            Text(
                                "UUID, hy2 password and subId → secrets.age.yaml",
                                style = MaterialTheme.typography.bodySmall,
                                color = Muted,
                                modifier = Modifier.padding(top = 8.dp),
                            )
                            Text(
                                "1 subscription at ${domain.ifEmpty { "the entry" }} · files on each entry",
                                style = MaterialTheme.typography.bodySmall,
                                color = Muted,
                                modifier = Modifier.padding(top = 4.dp),
                            )

                            if (expires().isNotEmpty()) {
                                Text(
                                    "expires ${expires()}",
                                    style = MaterialTheme.typography.bodySmall,
                                    color = Muted,
                                    modifier = Modifier.padding(top = 4.dp),
                                )
                            }

                            Spacer(Modifier.height(10.dp))

                            Box {
                                MonoText(
                                    "users: + ${name.trim()} ($mode)",
                                    color = OkText,
                                    style = MaterialTheme.typography.labelMedium,
                                )
                            }
                        }
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
                enabled = !busy && name.isNotBlank() && protos.isNotEmpty() && (term != "date…" || date.isNotBlank()),
                modifier = Modifier.fillMaxWidth(),
            )
        }
    }
}
