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
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * HostsScreen is the estate as the state describes it, split the way the job is:
 * entries take people in, exits are where the traffic leaves. Marker and
 * reachability come from a plan; latency and traffic do not exist in the core.
 */
@Composable
fun HostsScreen(ctx: android.content.Context, modifier: Modifier, onBack: () -> Unit) {
    val app = remember { Core.current() }
    var hosts by remember { mutableStateOf(emptyList<HostRow>()) }
    var links by remember { mutableStateOf(emptyList<LinkRow>()) }
    var plans by remember { mutableStateOf(mapOf<String, PlanHost>()) }
    var status by remember { mutableStateOf("") }
    var epoch by remember { mutableStateOf(0) }

    val head = remember(epoch) { app?.head().orEmpty() }

    LaunchedEffect(epoch) {
        hosts = withContext(Dispatchers.IO) { app?.let { parseHosts(it.hosts()) } ?: emptyList() }
        links = withContext(Dispatchers.IO) { app?.let { parseHostLinks(it.hostLinks()) } ?: emptyList() }

        status = onIo {
            val json = app?.plan("") ?: return@onIo "no state"
            plans = parsePlan(json).associateBy { it.name }
            ""
        }
    }

    val entries = hosts.filter { it.role == "entry" }
    val exits = hosts.filter { it.role != "entry" }
    val unreachable = plans.values.count { it.unreachable.isNotEmpty() }

    Column(modifier = modifier.fillMaxSize()) {
        ScreenHeader(
            title = "Hosts",
            subtitle = "${hosts.size} · ${entries.size} ${plural(entries.size, "entry", "entries")}, " +
                "${exits.size} ${plural(exits.size, "exit", "exits")}",
            onBack = onBack,
            actions = { IconAction(Icons.Outlined.Refresh, { epoch++ }, "Reload") },
        )

        LazyColumn(modifier = Modifier.weight(1f)) {
            if (entries.isNotEmpty()) {
                item {
                    SectionLabel("Entries · the way in")
                    SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp)) {
                        entries.forEachIndexed { index, host ->
                            if (index > 0) {
                                HorizontalDivider(color = Edge)
                            }

                            HostRowView(host, plans[host.name], head)
                        }
                    }
                }
            }

            if (exits.isNotEmpty()) {
                item {
                    SectionLabel("Exits · where traffic leaves")
                    SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp)) {
                        exits.forEachIndexed { index, host ->
                            if (index > 0) {
                                HorizontalDivider(color = Edge)
                            }

                            HostRowView(host, plans[host.name], head)
                        }
                    }
                }
            }

            item {
                SectionLabel("Links", "${links.size}")
                SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp)) {
                    links.forEachIndexed { index, link ->
                        if (index > 0) {
                            HorizontalDivider(color = Edge)
                        }

                        Row(
                            modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 14.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            MonoText(
                                "${link.from} → ${link.to}",
                                color = Ink,
                                style = MaterialTheme.typography.bodyMedium,
                                modifier = Modifier.weight(1f),
                            )

                            Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                                link.protocols.split(" ").filter { it.isNotEmpty() }.forEach { proto ->
                                    Pill(proto, if (proto == link.active) PillKind.OK else PillKind.PLAIN)
                                }
                            }
                        }
                    }
                }
            }

            item { Spacer(Modifier.height(24.dp)) }
        }

        if (status.isNotEmpty() || unreachable > 0) {
            Text(
                if (status.isNotEmpty()) status else "$unreachable unreachable",
                style = MaterialTheme.typography.bodySmall,
                color = if (status.isNotEmpty()) DangerText else Warn,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 6.dp),
            )
        }
    }
}
