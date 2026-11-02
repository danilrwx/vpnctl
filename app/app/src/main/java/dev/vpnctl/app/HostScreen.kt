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
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.CircleShape
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
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * HostScreen is one host as the state and a plan describe it: what it is, what
 * its entry serves, which links touch it, and what apply would change. Traffic,
 * machine load, the certificate and the inbound toggles the mockup draws are not
 * measured by the core, so the screen does not offer them (DESIGN.md correction 2).
 */
@Composable
fun HostScreen(ctx: android.content.Context, modifier: Modifier, name: String, onBack: () -> Unit) {
    val app = remember { Core.current() }
    var host by remember { mutableStateOf<HostRow?>(null) }
    var plan by remember { mutableStateOf<PlanHost?>(null) }
    var links by remember { mutableStateOf(emptyList<LinkRow>()) }
    var users by remember { mutableStateOf(emptyList<UserRow>()) }
    var served by remember { mutableStateOf(emptyList<String>()) }
    var status by remember { mutableStateOf("") }
    var epoch by remember { mutableStateOf(0) }

    val head = remember(epoch) { app?.head().orEmpty() }

    LaunchedEffect(name, epoch) {
        host = withContext(Dispatchers.IO) {
            app?.let { parseHosts(it.hosts()) }?.firstOrNull { it.name == name }
        }
        links = withContext(Dispatchers.IO) { app?.let { parseHostLinks(it.hostLinks()) } ?: emptyList() }
        users = withContext(Dispatchers.IO) { app?.let { parseUsers(it.users()) } ?: emptyList() }
        served = withContext(Dispatchers.IO) { servedProtocols(app) }

        status = onIo {
            val json = app?.plan(name) ?: return@onIo "no state"
            plan = parsePlan(json).firstOrNull { it.name == name }
            ""
        }
    }

    val h = host

    Column(modifier = modifier.fillMaxSize()) {
        ScreenHeader(
            title = name,
            subtitle = listOfNotNull(
                h?.role?.ifEmpty { null },
                h?.via?.takeIf { it.isNotEmpty() }?.let { "via $it" },
                h?.domain?.ifEmpty { null },
            ).joinToString(" · "),
            onBack = onBack,
            stacked = true,
        )

        LazyColumn(modifier = Modifier.weight(1f)) {
            item {
                Row(
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    when {
                        plan == null -> InfoChip("probing…", color = Warn)
                        plan!!.unreachable.isNotEmpty() -> InfoChip("no answer", color = DangerText)
                        plan!!.marker.isEmpty() -> InfoChip("no marker", color = Warn)
                        plan!!.marker == head -> InfoChip("${plan!!.marker} = HEAD", color = OkText)
                        else -> InfoChip("${plan!!.marker} · behind", color = Warn)
                    }

                    InfoChip(
                        "${plan?.changes ?: 0} ${plural(plan?.changes ?: 0, "change", "changes")}",
                        color = if ((plan?.changes ?: 0) > 0) Warn else Muted,
                    )

                    if (!h?.engine.isNullOrEmpty()) {
                        InfoChip(h!!.engine, color = Muted)
                    }
                }
            }

            if (h?.role == "entry") {
                item {
                    SectionLabel("Inbounds · what the entry serves")

                    SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp)) {
                        userProtocols(served).forEachIndexed { index, proto ->
                            if (index > 0) {
                                HorizontalDivider(color = Edge)
                            }

                            val count = users.count { proto in it.protocols }

                            Row(
                                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 14.dp),
                                verticalAlignment = Alignment.CenterVertically,
                            ) {
                                Text(proto, style = MaterialTheme.typography.titleSmall, modifier = Modifier.weight(1f))
                                Text(
                                    "$count ${plural(count, "user", "users")}",
                                    style = MaterialTheme.typography.bodySmall,
                                    color = Muted,
                                )
                            }
                        }
                    }
                }
            }

            val touching = links.filter { it.from == name || it.to == name }

            if (touching.isNotEmpty()) {
                item {
                    SectionLabel("Links", "${touching.size}")

                    SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp)) {
                        touching.forEachIndexed { index, link ->
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
            }

            item {
                SectionLabel("State")

                SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp)) {
                    Column(modifier = Modifier.fillMaxWidth().padding(16.dp)) {
                        listOfNotNull(
                            h?.ip?.ifEmpty { null }?.let { "ip" to it },
                            h?.via?.ifEmpty { null }?.let { "via" to it },
                            h?.domain?.ifEmpty { null }?.let { "domain" to it },
                            h?.role?.ifEmpty { null }?.let { "role" to it },
                            plan?.marker?.ifEmpty { null }?.let { "marker" to it },
                        ).forEach { (k, v) ->
                            Row(modifier = Modifier.fillMaxWidth().padding(vertical = 3.dp)) {
                                Text(k, style = MaterialTheme.typography.bodySmall, color = Muted, modifier = Modifier.width(64.dp))
                                MonoText(v, color = Ink, style = MaterialTheme.typography.bodySmall)
                            }
                        }

                        plan?.files?.filter { it.status != "=" }?.forEach { file ->
                            Row(modifier = Modifier.fillMaxWidth().padding(top = 2.dp)) {
                                Text(
                                    file.status,
                                    style = monoStyle(MaterialTheme.typography.labelMedium),
                                    color = when (file.status) {
                                        "+" -> OkText
                                        "-" -> DangerText
                                        "!" -> Warn
                                        else -> Warn
                                    },
                                    modifier = Modifier.width(16.dp),
                                )
                                Text(file.path, style = monoStyle(MaterialTheme.typography.labelMedium), color = Ink)
                            }
                        }

                        if (!plan?.unreachable.isNullOrEmpty()) {
                            Text(plan!!.unreachable, style = MaterialTheme.typography.bodySmall, color = DangerText)
                        }
                    }
                }
            }

            item {
                Text(
                    "editing, bootstrap and host actions stay on the command line · this screen reads the state",
                    style = MaterialTheme.typography.labelMedium,
                    color = Muted,
                    modifier = Modifier.padding(horizontal = 16.dp, vertical = 10.dp),
                )
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
