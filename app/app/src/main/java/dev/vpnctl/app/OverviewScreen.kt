package dev.vpnctl.app

import androidx.compose.foundation.background
import androidx.compose.foundation.border
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
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.CloudDownload
import androidx.compose.material.icons.outlined.PlaylistPlay
import androidx.compose.material.icons.outlined.VpnKey
import androidx.compose.material.icons.outlined.WarningAmber
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
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

/** RepoStateRow is what the git repository says about the state on screen. */
data class RepoStateRow(val branch: String = "", val clean: Boolean = true, val head: String = "")

/** parseRepoState reads mobile.RepoState; a state without a repository is empty. */
fun parseRepoState(json: String): RepoStateRow = try {
    val o = org.json.JSONObject(json)
    RepoStateRow(o.optString("branch"), o.optBoolean("clean", true), o.optString("head"))
} catch (e: Exception) {
    RepoStateRow()
}

/**
 * Overview is the estate at a glance: what the state says, what the probe found,
 * and which hosts are behind. Traffic, latency and CI are not in the core, so the
 * right column carries the marker and the change count (DESIGN.md correction 2).
 */
@Composable
fun OverviewScreen(
    ctx: android.content.Context,
    modifier: Modifier,
    onBack: () -> Unit,
    onPlan: () -> Unit,
    onDevices: () -> Unit,
) {
    val app = remember { Core.current() }
    val scope = rememberCoroutineScope()
    var users by remember { mutableStateOf(emptyList<UserRow>()) }
    var hosts by remember { mutableStateOf(emptyList<HostRow>()) }
    var links by remember { mutableStateOf(emptyList<LinkRow>()) }
    var plans by remember { mutableStateOf(mapOf<String, PlanHost>()) }
    var status by remember { mutableStateOf("") }
    var probing by remember { mutableStateOf(false) }
    var pulling by remember { mutableStateOf(false) }
    var repo by remember { mutableStateOf(RepoStateRow()) }
    var epoch by remember { mutableStateOf(0) }

    val head = repo.head

    LaunchedEffect(epoch) {
        repo = withContext(Dispatchers.IO) { parseRepoState(app?.repoState() ?: "{}") }

        users = withContext(Dispatchers.IO) { app?.let { parseUsers(it.users()) } ?: emptyList() }
        hosts = withContext(Dispatchers.IO) { app?.let { parseHosts(it.hosts()) } ?: emptyList() }
        links = withContext(Dispatchers.IO) { app?.let { parseHostLinks(it.hostLinks()) } ?: emptyList() }

        probing = true
        status = onIo {
            val json = app?.plan("") ?: return@onIo "no state"
            plans = parsePlan(json).associateBy { it.name }
            ""
        }
        probing = false
    }

    val active = users.count { it.enabled }
    val entries = hosts.count { it.role == "entry" }
    val exits = hosts.count { it.role == "exit" }
    val unreachable = plans.values.count { it.unreachable.isNotEmpty() }
    val changes = plans.values.sumOf { it.changes }
    val behind = plans.values.count { it.marker.isNotEmpty() && head.isNotEmpty() && it.marker != head }

    val attention = plans.values.filter { it.unreachable.isNotEmpty() || it.changes > 0 || it.marker != head }

    Box(modifier = modifier.fillMaxSize()) {
        LazyColumn(modifier = Modifier.fillMaxSize()) {
            item {
                ScreenHeader(
                    title = "vpnctl",
                    subtitle = if (head.isEmpty()) "no commit" else "state $head",
                    actions = {
                        IconAction(
                            Icons.Outlined.CloudDownload,
                            {
                                if (!pulling) {
                                    pulling = true
                                    status = "pulling…"

                                    scope.launch {
                                        status = onIo { app?.pull()?.let { "pulled $it" } ?: "no repository" }
                                        pulling = false
                                        epoch++
                                    }
                                }
                            },
                            "Pull",
                            tint = if (pulling) Muted else Link,
                        )
                        IconAction(Icons.Outlined.VpnKey, onDevices, "Devices and keys")
                        IconAction(Icons.Outlined.Refresh, { epoch++ }, "Reload")
                    },
                )
            }

            item {
                Row(
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    InfoChip(
                        if (repo.branch.isEmpty()) "no repository" else "${repo.branch} · ${if (repo.clean) "clean" else "dirty"}",
                        color = if (repo.clean) Muted else Warn,
                    )
                    InfoChip("HEAD ${head.ifEmpty { "—" }}", color = Muted)
                    InfoChip(
                        when {
                            probing -> "probing…"
                            changes > 0 -> "$changes changes"
                            else -> "in sync"
                        },
                        color = when {
                            probing -> Warn
                            changes > 0 -> Warn
                            else -> OkText
                        },
                    )
                }
            }

            if (attention.isNotEmpty()) {
                item {
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(horizontal = 16.dp, vertical = 12.dp)
                            .clip(RoundedCornerShape(16.dp))
                            .background(WarnSoft)
                            .border(1.dp, Warn.copy(alpha = 0.45f), RoundedCornerShape(16.dp))
                            .clickable(onClick = onPlan)
                            .padding(16.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Icon(Icons.Outlined.WarningAmber, contentDescription = null, tint = Warn, modifier = Modifier.size(22.dp))

                        Column(modifier = Modifier.weight(1f).padding(start = 12.dp)) {
                            Text("Needs attention", style = MaterialTheme.typography.titleSmall, color = Warn)
                            Text(
                                attention.take(2).joinToString(" · ") { host ->
                                    when {
                                        host.unreachable.isNotEmpty() -> "${host.name} · no answer"
                                        host.changes > 0 -> "${host.name} · ${host.changes} changes"
                                        else -> "${host.name} · behind"
                                    }
                                } + if (attention.size > 2) " · +${attention.size - 2}" else "",
                                style = MaterialTheme.typography.bodySmall,
                                color = Ink,
                            )
                        }

                        Text("Plan", style = MaterialTheme.typography.titleSmall, color = Link)
                    }
                }
            }

            item {
                SectionLabel(
                    "Hosts",
                    "$entries ${plural(entries, "entry", "entries")} · $exits ${plural(exits, "exit", "exits")}" +
                        if (unreachable > 0) " · $unreachable unreachable" else "",
                )

                SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp)) {
                    hosts.forEachIndexed { index, host ->
                        if (index > 0) {
                            HorizontalDivider(color = Edge)
                        }

                        HostRowView(host, plans[host.name], head)
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

            item {
                Row(
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp),
                    horizontalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    StatCard("Users", "$active", "${users.size} total", modifier = Modifier.weight(1f))
                    StatCard(
                        "Hosts",
                        if (probing) "—" else "${hosts.size}",
                        when {
                            probing -> "probing…"
                            unreachable > 0 -> "$unreachable unreachable"
                            else -> "all answer"
                        },
                        modifier = Modifier.weight(1f),
                    )
                    StatCard(
                        "Changes",
                        if (probing) "—" else "$changes",
                        when {
                            probing -> "probing…"
                            behind > 0 -> "$behind behind"
                            else -> "on apply"
                        },
                        valueColor = if (!probing && changes > 0) Warn else Ink,
                        modifier = Modifier.weight(1f),
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

            item { Spacer(Modifier.height(96.dp)) }
        }

        Box(modifier = Modifier.align(Alignment.BottomEnd).padding(end = 18.dp, bottom = 18.dp)) {
            ActionPill(
                onClick = onPlan,
                icon = Icons.Outlined.PlaylistPlay,
                text = "Plan · ${hosts.size} hosts",
            )
        }
    }
}

/** hostTone is the dot: error for no answer, warn for work, ok for in sync. */
fun hostTone(plan: PlanHost?, head: String): Color = when {
    plan == null -> Muted
    plan.unreachable.isNotEmpty() || !plan.bootstrapped -> Danger
    plan.changes > 0 || (plan.marker.isNotEmpty() && head.isNotEmpty() && plan.marker != head) -> Warn
    else -> Ok
}

@Composable
fun HostRowView(host: HostRow, plan: PlanHost?, head: String) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        StatusDot(hostTone(plan, head), size = 10.dp)

        Column(modifier = Modifier.weight(1f).padding(start = 12.dp)) {
            Text(host.name, style = MaterialTheme.typography.titleLarge, maxLines = 1)

            Text(
                listOfNotNull(
                    host.role,
                    host.via.takeIf { it.isNotEmpty() }?.let { "via $it" },
                    host.engine.ifEmpty { null },
                    host.domain.ifEmpty { null },
                ).joinToString(" · "),
                style = MaterialTheme.typography.bodySmall,
                color = Muted,
                maxLines = 1,
            )

            MonoText(
                when {
                    plan == null -> "not probed"
                    plan.unreachable.isNotEmpty() -> plan.unreachable
                    !plan.bootstrapped -> "bootstrap not done"
                    plan.marker.isEmpty() -> "no marker"
                    else -> "${plan.marker} ${if (plan.marker == head) "= HEAD" else "· behind"}"
                },
                color = when {
                    plan == null -> Muted
                    plan.unreachable.isNotEmpty() -> DangerText
                    plan.marker.isNotEmpty() && plan.marker != head -> Warn
                    else -> OkText
                },
                style = MaterialTheme.typography.labelMedium,
                modifier = Modifier.padding(top = 2.dp),
            )
        }

        if (host.retiring) {
            Pill("retiring", PillKind.PLAIN)
        }

        Column(horizontalAlignment = Alignment.End, modifier = Modifier.padding(start = 8.dp)) {
            Text(
                plan?.let { "${it.changes}" } ?: "—",
                style = monoStyle(MaterialTheme.typography.bodyMedium),
                color = when {
                    plan == null -> Muted
                    plan.changes > 0 -> Warn
                    else -> Ink
                },
            )
            Text(
                if (plan != null && plan.changes > 0) "to apply" else "changes",
                style = MaterialTheme.typography.labelMedium,
                color = Muted,
            )
        }
    }
}
