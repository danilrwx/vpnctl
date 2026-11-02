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
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * AddHostScreen creates a host with its inbound, keys and routing, the way
 * `vpnctl host add` does. Bootstrap is not here: it needs the host to answer,
 * and the phone can only reach a host through the agent the bootstrap installs.
 */
@Composable
fun AddHostScreen(ctx: android.content.Context, modifier: Modifier, onBack: () -> Unit, onCreated: () -> Unit) {
    val app = remember { Core.current() }
    val scope = rememberCoroutineScope()

    var hosts by remember { mutableStateOf(emptyList<HostRow>()) }
    var name by remember { mutableStateOf("") }
    var role by remember { mutableStateOf("exit") }
    var ssh by remember { mutableStateOf("") }
    var ip by remember { mutableStateOf("") }
    var domain by remember { mutableStateOf("") }
    var via by remember { mutableStateOf("") }
    var engine by remember { mutableStateOf("sing-box") }
    var awg by remember { mutableStateOf(false) }
    var status by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }

    LaunchedEffect(Unit) {
        hosts = withContext(Dispatchers.IO) { app?.let { parseHosts(it.hosts()) } ?: emptyList() }
        via = hosts.firstOrNull { it.role == "entry" }?.name.orEmpty()
    }

    fun create() {
        if (busy || name.isBlank() || ssh.isBlank()) {
            return
        }

        busy = true
        status = "…"

        scope.launch {
            val out = onIo { addHostText(app, name.trim(), role, ssh.trim(), ip.trim(), domain.trim(), via, engine, awg) }

            busy = false

            if (out.startsWith("ok")) {
                onCreated()
            } else {
                status = out
            }
        }
    }

    Column(modifier = modifier.fillMaxSize()) {
        ScreenHeader(
            title = "New host",
            subtitle = "keys and routing now, bootstrap on the command line",
            onBack = onBack,
            stacked = true,
        )

        LazyColumn(modifier = Modifier.weight(1f)) {
            item {
                SurfaceCard(modifier = Modifier.padding(16.dp)) {
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

                        Text("SSH", style = MaterialTheme.typography.titleSmall, modifier = Modifier.padding(top = 12.dp))
                        OutlinedTextField(
                            value = ssh,
                            onValueChange = { ssh = it },
                            label = { Text("root@203.0.113.10") },
                            singleLine = true,
                            textStyle = MaterialTheme.typography.bodyMedium,
                            colors = appFieldColors(),
                            modifier = Modifier.fillMaxWidth().padding(top = 8.dp),
                        )

                        Text("Address", style = MaterialTheme.typography.titleSmall, modifier = Modifier.padding(top = 12.dp))
                        OutlinedTextField(
                            value = ip,
                            onValueChange = { ip = it },
                            label = { Text("IP, empty — take it from SSH") },
                            singleLine = true,
                            textStyle = MaterialTheme.typography.bodyMedium,
                            colors = appFieldColors(),
                            modifier = Modifier.fillMaxWidth().padding(top = 8.dp),
                        )
                        OutlinedTextField(
                            value = domain,
                            onValueChange = { domain = it },
                            label = { Text("domain, for an entry") },
                            singleLine = true,
                            textStyle = MaterialTheme.typography.bodyMedium,
                            colors = appFieldColors(),
                            modifier = Modifier.fillMaxWidth().padding(top = 8.dp),
                        )
                    }
                }
            }

            item {
                SectionLabel("Role")
                Segmented(listOf("entry", "exit"), role) { role = it }
                Text(
                    "an entry takes people in and needs a domain; an exit is where traffic leaves",
                    style = MaterialTheme.typography.labelMedium,
                    color = Muted,
                    modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                )
            }

            item {
                SectionLabel("Engine")
                Segmented(listOf("sing-box", "xray"), engine) { engine = it }
            }

            if (hosts.isNotEmpty()) {
                item {
                    SectionLabel("Via")
                    Row(
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        hosts.forEach { host ->
                            Chip(host.name, { via = if (via == host.name) "" else host.name }, selected = via == host.name)
                        }
                    }
                    Text(
                        "the host the phone reaches this one through; empty for a direct entry",
                        style = MaterialTheme.typography.labelMedium,
                        color = Muted,
                        modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                    )
                }
            }

            item {
                SectionLabel("Transports")

                SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp)) {
                    Row(
                        modifier = Modifier.fillMaxWidth().padding(16.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Column(modifier = Modifier.weight(1f)) {
                            Text("AmneziaWG", style = MaterialTheme.typography.titleSmall)
                            Text(
                                "a tunnel for the node links; entries also get split/full subnets",
                                style = MaterialTheme.typography.bodySmall,
                                color = Muted,
                            )
                        }

                        Switch(
                            checked = awg,
                            onCheckedChange = { awg = it },
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
                text = if (busy) "creating…" else "Create host",
                onClick = { create() },
                enabled = !busy && name.isNotBlank() && ssh.isNotBlank(),
                modifier = Modifier.fillMaxWidth(),
            )
        }
    }
}

/** addHostText creates the host, its files and its keys. */
fun addHostText(
    app: mobile.App?,
    name: String,
    role: String,
    ssh: String,
    ip: String,
    domain: String,
    via: String,
    engine: String,
    awg: Boolean,
): String {
    if (app == null) {
        return "no state"
    }

    return try {
        "ok: ${app.addHost(name, role, ssh, ip, domain, via, engine, awg)} · bootstrap it from the command line"
    } catch (e: Exception) {
        "error: ${e.message}"
    }
}
