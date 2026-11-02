package dev.vpnctl.app

import androidx.compose.foundation.clickable
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
import androidx.compose.material.icons.outlined.Add
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
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * HostsScreen is the estate as the state describes it, split the way the job is:
 * entries take people in, exits are where the traffic leaves. Marker and
 * reachability come from a plan; latency and traffic do not exist in the core.
 */
@Composable
fun HostsScreen(
    ctx: android.content.Context,
    modifier: Modifier,
    onBack: () -> Unit,
    onPick: (String) -> Unit,
    onAddHost: () -> Unit,
) {
    val app = remember { Core.current() }
    var hosts by remember { mutableStateOf(emptyList<HostRow>()) }
    var links by remember { mutableStateOf(emptyList<LinkRow>()) }
    var plans by remember { mutableStateOf(mapOf<String, PlanHost>()) }
    var status by remember { mutableStateOf("") }
    var epoch by remember { mutableStateOf(0) }
    var linking by remember { mutableStateOf(false) }
    var linkFrom by remember { mutableStateOf("") }
    var linkTo by remember { mutableStateOf("") }
    var linkProtos by remember { mutableStateOf(setOf("awg")) }
    var linkingBusy by remember { mutableStateOf(false) }
    var editingLink by remember { mutableStateOf<LinkRow?>(null) }
    var editProtos by remember { mutableStateOf(emptySet<String>()) }
    var editActive by remember { mutableStateOf("") }
    val scope = androidx.compose.runtime.rememberCoroutineScope()

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
            actions = {
                IconAction(Icons.Outlined.Add, onAddHost, "Add host")
                IconAction(Icons.Outlined.Refresh, { epoch++ }, "Reload")
            },
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

                            HostRowView(host, plans[host.name], head, onClick = { onPick(host.name) })
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

                            HostRowView(host, plans[host.name], head, onClick = { onPick(host.name) })
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
                            modifier = Modifier
                                .fillMaxWidth()
                                .clickable {
                                    editingLink = link
                                    editProtos = link.protocols.split(" ").filter { it.isNotEmpty() }.toSet()
                                    editActive = link.active
                                }
                                .padding(horizontal = 16.dp, vertical = 14.dp),
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

                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clickable {
                                linkFrom = ""
                                linkTo = ""
                                linkProtos = setOf("awg")
                                linking = true
                            }
                            .padding(horizontal = 16.dp, vertical = 12.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text("+ New link", style = MaterialTheme.typography.bodyMedium, color = Link)
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

    editingLink?.let { link ->
        val was = link.protocols.split(" ").filter { it.isNotEmpty() }.toSet()
        val add = (editProtos - was).joinToString(",")
        val rm = (was - editProtos).joinToString(",")

        AppDialog(
            title = "${link.from} → ${link.to}",
            onDismiss = { editingLink = null },
            confirmLabel = "Save",
            confirmEnabled = !linkingBusy && editProtos.isNotEmpty() &&
                (add.isNotEmpty() || rm.isNotEmpty() || editActive != link.active),
            onConfirm = {
                linkingBusy = true

                scope.launch {
                    status = onIo { setLinkText(Core.current(), link.from, link.to, add, rm, editActive) }
                    linkingBusy = false

                    if (status.startsWith("ok")) {
                        editingLink = null
                        epoch++
                    }
                }
            },
            content = {
                Text(
                    "removing a transport from the link does not delete its keys; adding one back reuses them",
                    style = MaterialTheme.typography.bodySmall,
                    color = Muted,
                )

                Text("protocols", style = MaterialTheme.typography.labelMedium, color = Muted, modifier = Modifier.padding(top = 10.dp, bottom = 4.dp))
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    listOf("awg", "reality", "hy2").forEach { proto ->
                        Chip(
                            proto,
                            onClick = {
                                editProtos = if (proto in editProtos) editProtos - proto else editProtos + proto
                                if (editActive == proto && proto !in editProtos) {
                                    editActive = editProtos.firstOrNull().orEmpty()
                                }
                            },
                            selected = proto in editProtos,
                        )
                    }
                }

                Text("active", style = MaterialTheme.typography.labelMedium, color = Muted, modifier = Modifier.padding(top = 10.dp, bottom = 4.dp))
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    editProtos.sorted().forEach { proto ->
                        Chip(proto, { editActive = proto }, selected = editActive == proto)
                    }
                }
            },
        )
    }

    if (linking) {
        AppDialog(
            title = "New link",
            onDismiss = { linking = false },
            confirmLabel = if (linkingBusy) "adding…" else "Add",
            confirmEnabled = !linkingBusy && linkFrom.isNotEmpty() && linkTo.isNotEmpty() &&
                linkFrom != linkTo && linkProtos.isNotEmpty(),
            onConfirm = {
                linkingBusy = true

                scope.launch {
                    status = onIo {
                        addLinkText(
                            Core.current(),
                            linkFrom,
                            linkTo,
                            linkProtos.sorted().joinToString(","),
                        )
                    }
                    linkingBusy = false

                    if (status.startsWith("ok")) {
                        linking = false
                        epoch++
                    }
                }
            },
            content = {
                Text(
                    "the chain the exits use; protocols are the transports both sides will run",
                    style = MaterialTheme.typography.bodySmall,
                    color = Muted,
                )

                // a link runs from an entry to an exit: offering the other
                // direction is offering a state the render rejects
                val fromHosts = hosts.filter { it.role == "entry" }.ifEmpty { hosts }
                val toHosts = hosts.filter { it.role != "entry" }.ifEmpty { hosts }

                Text("from", style = MaterialTheme.typography.labelMedium, color = Muted, modifier = Modifier.padding(top = 10.dp, bottom = 4.dp))
                fromHosts.chunked(2).forEach { pair ->
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.padding(bottom = 6.dp)) {
                        pair.forEach { host ->
                            Chip(host.name, { linkFrom = host.name }, selected = linkFrom == host.name)
                        }
                    }
                }

                Text("to", style = MaterialTheme.typography.labelMedium, color = Muted, modifier = Modifier.padding(top = 4.dp, bottom = 4.dp))
                toHosts.chunked(2).forEach { pair ->
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.padding(bottom = 6.dp)) {
                        pair.forEach { host ->
                            Chip(host.name, { linkTo = host.name }, selected = linkTo == host.name)
                        }
                    }
                }

                Text("protocols", style = MaterialTheme.typography.labelMedium, color = Muted, modifier = Modifier.padding(top = 4.dp, bottom = 4.dp))
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    listOf("awg", "reality", "hy2").forEach { proto ->
                        Chip(
                            proto,
                            onClick = {
                                linkProtos = if (proto in linkProtos) linkProtos - proto else linkProtos + proto
                            },
                            selected = proto in linkProtos,
                        )
                    }
                }
            },
        )
    }
}

/** setLinkText applies the protocol set the edit dialog produced. */
fun setLinkText(app: mobile.App?, from: String, to: String, add: String, rm: String, active: String): String {
    if (app == null) {
        return "no state"
    }

    return try {
        "ok: ${app.setLink(from, to, add, rm, active, "")} · push when the forge is reachable"
    } catch (e: Exception) {
        "error: ${e.message}"
    }
}

/** addLinkText appends a link with fresh link secrets. */
fun addLinkText(app: mobile.App?, from: String, to: String, protocols: String): String {
    if (app == null) {
        return "no state"
    }

    val first = protocols.substringBefore(",")

    return try {
        "ok: ${app.addLink(from, to, protocols, first, "auto")} · push when the forge is reachable"
    } catch (e: Exception) {
        "error: ${e.message}"
    }
}
