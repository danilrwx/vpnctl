package dev.vpnctl.app

import androidx.compose.foundation.background
import androidx.compose.foundation.border
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
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
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
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * Topology is the estate as the state draws it: the entry at the top, the exits
 * below, and one card per link. Latency, probe history and the static checks the
 * mockup lists are not measured here, so the screen does not pretend to show
 * them (DESIGN.md correction 2).
 */
@Composable
fun TopologyScreen(ctx: android.content.Context, modifier: Modifier, onBack: () -> Unit) {
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
    val domain = entries.firstOrNull { it.domain.isNotEmpty() }?.domain.orEmpty()
    val unreachable = plans.values.filter { it.unreachable.isNotEmpty() }

    Column(modifier = modifier.fillMaxSize()) {
        ScreenHeader(
            title = "Topology",
            subtitle = "${links.size} ${plural(links.size, "link", "links")} · ${hosts.size} hosts",
            onBack = onBack,
            actions = { IconAction(Icons.Outlined.Refresh, { epoch++ }, "Reload") },
        )

        LazyColumn(modifier = Modifier.weight(1f)) {
            if (hosts.isEmpty()) {
                item {
                    Text(
                        "no hosts described: the diagram has nothing to draw until a host is added",
                        style = MaterialTheme.typography.bodySmall,
                        color = Muted,
                        modifier = Modifier.padding(16.dp),
                    )
                }
            }

            item {
                SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) {
                    Column(
                        modifier = Modifier.fillMaxWidth().padding(16.dp),
                        horizontalAlignment = Alignment.CenterHorizontally,
                    ) {
                        if (domain.isNotEmpty()) {
                            InfoChip(domain, color = Link)
                            Box(modifier = Modifier.width(1.dp).height(16.dp).background(Edge))
                        }

                        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                            entries.forEach { entry ->
                                TopologyNode(
                                    host = entry,
                                    plan = plans[entry.name],
                                    head = head,
                                    subtitle = "entry · jump",
                                )
                            }
                        }

                        Box(modifier = Modifier.width(1.dp).height(16.dp).background(Edge))

                        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                            exits.forEach { exit ->
                                // a link names the protocol it pumped through; an
                                // exit no link carries traffic through is standby
                                val carried = links.any { it.to == exit.name && it.active.isNotEmpty() }

                                TopologyNode(
                                    host = exit,
                                    plan = plans[exit.name],
                                    head = head,
                                    subtitle = "exit · " + if (carried) "active" else "standby",
                                )
                            }
                        }

                        Text(
                            "drawn from the state · the hosts answer for themselves in the links below",
                            style = MaterialTheme.typography.labelMedium,
                            color = Muted,
                            modifier = Modifier.padding(top = 14.dp),
                        )
                    }
                }
            }

            items(links.size) { index ->
                val link = links[index]

                SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp, vertical = 6.dp)) {
                    Column(modifier = Modifier.fillMaxWidth().padding(16.dp)) {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            MonoText(
                                "${link.from} → ${link.to}",
                                color = Ink,
                                style = MaterialTheme.typography.bodyMedium,
                                modifier = Modifier.weight(1f),
                            )
                            Text(
                                if (link.active.isNotEmpty()) "active" else "standby",
                                style = MaterialTheme.typography.bodySmall,
                                color = if (link.active.isNotEmpty()) OkText else Muted,
                            )
                        }

                        Row(
                            modifier = Modifier.padding(top = 10.dp),
                            horizontalArrangement = Arrangement.spacedBy(8.dp),
                        ) {
                            link.protocols.split(" ").filter { it.isNotEmpty() }.forEach { proto ->
                                Pill(proto, if (proto == link.active) PillKind.OK else PillKind.PLAIN)
                            }
                        }
                    }
                }
            }

            if (unreachable.isNotEmpty()) {
                item {
                    Text(
                        unreachable.joinToString(" · ") { "✗ ${it.name}: ${it.unreachable}" },
                        style = MaterialTheme.typography.bodySmall,
                        color = DangerText,
                        modifier = Modifier.padding(horizontal = 16.dp, vertical = 12.dp),
                    )
                }
            }

            if (status.isNotEmpty()) {
                item {
                    Text(
                        status,
                        style = MaterialTheme.typography.bodySmall,
                        color = DangerText,
                        modifier = Modifier.padding(horizontal = 16.dp),
                    )
                }
            }

            item { Spacer(Modifier.height(24.dp)) }
        }
    }
}

/** TopologyNode is one host in the diagram: solid when it answers, dashed when
 * it is described by the state but not yet ready. */
@Composable
fun TopologyNode(host: HostRow, plan: PlanHost?, head: String, subtitle: String) {
    val ready = plan != null && plan.unreachable.isEmpty() && plan.bootstrapped
    val dashed = !ready
    val tone: Color = when {
        !ready -> Danger
        plan?.changes != null && plan.changes > 0 -> Warn
        else -> Ok
    }

    Column(
        modifier = Modifier
            .clip(RoundedCornerShape(12.dp))
            .background(Surface1)
            .then(
                if (dashed) Modifier.dashedBorder(tone) else Modifier.border(1.dp, tone, RoundedCornerShape(12.dp)),
            )
            .padding(horizontal = 16.dp, vertical = 10.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        MonoText(host.name, color = Ink, style = MaterialTheme.typography.bodyMedium)
        Text(subtitle, style = MaterialTheme.typography.labelMedium, color = Muted, modifier = Modifier.padding(top = 2.dp))
    }
}
