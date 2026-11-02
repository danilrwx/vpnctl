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
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.PlayArrow
import androidx.compose.material.icons.outlined.Refresh
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
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * PlanScreen says what apply would do, host by host, in the order the series
 * runs: exits first, the jump host last. The preflight card is the same
 * information plan gives on the command line — a change count, the notes, and
 * the hosts that will be skipped.
 */
@Composable
fun PlanScreen(ctx: android.content.Context, modifier: Modifier, onBack: () -> Unit) {
    val app = remember { Core.current() }
    val scope = rememberCoroutineScope()

    var status by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var hosts by remember { mutableStateOf(emptyList<PlanHost>()) }
    var roles by remember { mutableStateOf(mapOf<String, String>()) }
    var log by remember { mutableStateOf(emptyList<String>()) }
    var probing by remember { mutableStateOf(true) }

    val head = remember { app?.head().orEmpty() }
    val files = hosts.sumOf { h -> h.files.count { it.status != "=" } }
    val changed = hosts.count { it.changes > 0 || it.unreachable.isNotEmpty() }

    suspend fun load() {
        probing = true
        status = onIo {
            val json = app?.plan("") ?: return@onIo "no state"
            hosts = parsePlan(json)
            ""
        }
        probing = false
    }

    // One action at a time: the hosts see a series of requests and a second apply
    // would interleave with the first.
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

    LaunchedEffect(Unit) {
        roles = withContext(Dispatchers.IO) {
            app?.let { parseHosts(it.hosts()) }?.associate { it.name to it.role }.orEmpty()
        }

        load()
    }

    Column(modifier = modifier.fillMaxSize()) {
        ScreenHeader(
            title = "Plan",
            subtitle = "${head.ifEmpty { "—" }} · ${hosts.size} hosts, $files ${plural(files, "file", "files")}",
            onBack = onBack,
            backIcon = Icons.Outlined.Close,
            actions = { IconAction(Icons.Outlined.Refresh, { scope.launch { load() } }, "Reload") },
        )

        LazyColumn(modifier = Modifier.weight(1f)) {
            item {
                SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) {
                    Column(modifier = Modifier.fillMaxWidth().padding(16.dp)) {
                        if (probing) {
                            Text("probing every host…", style = MaterialTheme.typography.bodySmall, color = Muted)
                        }

                        when {
                            hosts.isEmpty() && !probing -> {
                                Text("nothing to plan", style = MaterialTheme.typography.bodySmall, color = Muted)
                            }
                            changed == 0 -> {
                                Text("✓ every host is at $head", style = MaterialTheme.typography.bodySmall, color = OkText)
                            }
                            else -> {
                                Text(
                                    "· $changed of ${hosts.size} hosts have work · $files files",
                                    style = MaterialTheme.typography.bodySmall,
                                    color = Ink,
                                )
                            }
                        }

                        hosts.filter { it.unreachable.isNotEmpty() }.forEach { host ->
                            Text(
                                "! ${host.name}: ${host.unreachable}",
                                style = MaterialTheme.typography.bodySmall,
                                color = DangerText,
                                modifier = Modifier.padding(top = 4.dp),
                            )
                        }

                        hosts.filter { !it.bootstrapped && it.unreachable.isEmpty() }.forEach { host ->
                            Text(
                                "! ${host.name}: bootstrap not done, apply will skip it",
                                style = MaterialTheme.typography.bodySmall,
                                color = Warn,
                                modifier = Modifier.padding(top = 4.dp),
                            )
                        }
                    }
                }
            }

            itemsIndexed(hosts) { index, host ->
                HostPlanCard(index + 1, host, roles[host.name].orEmpty(), head)
            }

            if (log.isNotEmpty()) {
                item {
                    SectionLabel("Apply log")

                    SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp)) {
                        Text(
                            log.takeLast(60).joinToString("\n"),
                            style = monoStyle(MaterialTheme.typography.labelMedium),
                            color = Muted,
                            modifier = Modifier.padding(16.dp),
                        )
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
                    status == "applied" -> OkText
                    else -> Muted
                },
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 6.dp),
            )
        }

        Column(modifier = Modifier.fillMaxWidth().padding(16.dp)) {
            Text(
                "exits first, the jump host last · stop and roll back if the probe fails",
                style = MaterialTheme.typography.labelMedium,
                color = Muted,
                modifier = Modifier.padding(bottom = 10.dp),
            )

            Row(verticalAlignment = Alignment.CenterVertically) {
                PrimaryButton(
                    text = if (busy) "applying…" else "Apply ${hosts.size} hosts",
                    onClick = {
                        act {
                            log = emptyList()

                            val out = applyWithProgress(app, "") { line -> log = log + line }

                            // the hosts just changed: show what is left, not what was
                            if (out == "applied") {
                                load()
                            }

                            out
                        }
                    },
                    enabled = !busy && hosts.isNotEmpty(),
                    modifier = Modifier.weight(1f),
                )
            }
        }
    }
}

/** HostPlanCard is one numbered host: what changes, and what the probe said. */
@Composable
fun HostPlanCard(number: Int, host: PlanHost, role: String, head: String) {
    SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp, vertical = 6.dp)) {
        Row(
            modifier = Modifier.fillMaxWidth().padding(16.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(
                modifier = Modifier.size(28.dp).clip(CircleShape).background(Surface2),
                contentAlignment = Alignment.Center,
            ) {
                Text("$number", style = MaterialTheme.typography.labelMedium, color = Muted)
            }

            Column(modifier = Modifier.weight(1f).padding(start = 12.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(host.name, style = MaterialTheme.typography.titleLarge, maxLines = 1)

                    if (role.isNotEmpty()) {
                        Text(
                            "  $role",
                            style = MaterialTheme.typography.bodySmall,
                            color = Muted,
                        )
                    }

                    Spacer(Modifier.width(6.dp))

                    if (host.unreachable.isNotEmpty()) {
                        Text(
                            "  no answer",
                            style = MaterialTheme.typography.bodySmall,
                            color = DangerText,
                        )
                    } else if (!host.bootstrapped) {
                        Text(
                            "  skipped",
                            style = MaterialTheme.typography.bodySmall,
                            color = Warn,
                        )
                    } else if (host.changes == 0) {
                        Text(
                            "  unchanged",
                            style = MaterialTheme.typography.bodySmall,
                            color = OkText,
                        )
                    }
                }

                MonoText(
                    if (host.marker.isEmpty()) "no marker → ${head.ifEmpty { "—" }}" else "${host.marker} → ${head.ifEmpty { "—" }}",
                    color = Muted,
                    style = MaterialTheme.typography.labelMedium,
                    modifier = Modifier.padding(top = 2.dp),
                )
            }
        }

        host.files.filter { it.status != "=" }.forEach { file ->
            Row(modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 2.dp)) {
                Text(
                    file.status,
                    style = monoStyle(MaterialTheme.typography.labelMedium),
                    color = when (file.status) {
                        "+" -> OkText
                        "-" -> DangerText
                        else -> Warn
                    },
                    modifier = Modifier.width(16.dp),
                )
                Text(file.path, style = monoStyle(MaterialTheme.typography.bodySmall), color = Ink, modifier = Modifier.weight(1f))
            }
        }

        if (host.notes.isNotEmpty()) {
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(start = 16.dp, end = 16.dp, top = 8.dp, bottom = 16.dp)
                    .clip(RoundedCornerShape(12.dp))
                    .background(WarnSoft)
                    .padding(12.dp),
            ) {
                host.notes.forEach { note ->
                    Text(note, style = MaterialTheme.typography.bodySmall, color = Warn)
                }
            }
        } else {
            Spacer(Modifier.height(12.dp))
        }
    }
}
