package dev.vpnctl.app

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.height
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
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material3.HorizontalDivider
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
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
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
    var metrics by remember { mutableStateOf<MetricsRow?>(null) }
    var inbounds by remember { mutableStateOf(emptyList<InboundRow>()) }
    var toggling by remember { mutableStateOf("") }
    var certs by remember { mutableStateOf(emptyList<CertRow>()) }
    var certNote by remember { mutableStateOf("") }
    var series by remember { mutableStateOf(emptyList<MetricsRow>()) }
    var metricsNote by remember { mutableStateOf("") }
    var editing by remember { mutableStateOf(false) }
    var editSSH by remember { mutableStateOf("") }
    var editIP by remember { mutableStateOf("") }
    var editDomain by remember { mutableStateOf("") }
    var editVia by remember { mutableStateOf("") }
    var editEngine by remember { mutableStateOf("sing-box") }
    var editBusy by remember { mutableStateOf(false) }

    val head = remember(epoch) { app?.head().orEmpty() }
    val scope = androidx.compose.runtime.rememberCoroutineScope()

    LaunchedEffect(name, epoch) {
        host = withContext(Dispatchers.IO) {
            app?.let { parseHosts(it.hosts()) }?.firstOrNull { it.name == name }
        }
        links = withContext(Dispatchers.IO) { app?.let { parseHostLinks(it.hostLinks()) } ?: emptyList() }
        users = withContext(Dispatchers.IO) { app?.let { parseUsers(it.users()) } ?: emptyList() }
        served = withContext(Dispatchers.IO) { servedProtocols(app) }

        // three questions to the same host, each its own signed request: run
        // them together, or the screen spends a handshake per card
        kotlinx.coroutines.coroutineScope {
            launch {
                status = onIo {
                    val json = app?.plan(name) ?: return@onIo "no state"
                    plan = parsePlan(json).firstOrNull { it.name == name }
                    ""
                }
            }

            launch {
                inbounds = withContext(Dispatchers.IO) {
                    app?.let { parseInbounds(it.inbounds(name)) } ?: emptyList()
                }
            }

            launch {
                val certJSON = onIo { app?.certs(name) ?: "no state" }
                certs = if (certJSON.startsWith("error")) emptyList() else parseCerts(certJSON)

                if (certJSON.startsWith("error")) {
                    certNote = certJSON
                } else if (certs.isEmpty()) {
                    certNote = "no certificates on this host: only the entry runs Caddy"
                }
            }

            launch {
                val lines = onIo { app?.metrics(name) ?: "no state" }

                if (lines.startsWith("error")) {
                    metricsNote = lines
                } else {
                    series = parseMetricsSeries(lines)
                    metrics = series.lastOrNull()

                    if (metrics == null) {
                        metricsNote = "no samples yet: the timer writes one every 5 minutes"
                    }
                }
            }
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
            actions = {
                IconAction(Icons.Outlined.Edit, {
                    val row = host
                    if (row != null) {
                        editSSH = ""
                        editIP = row.ip
                        editDomain = row.domain
                        editVia = row.via
                        editEngine = row.engine.ifEmpty { "sing-box" }
                        editing = true
                    }
                }, "Edit")
            },
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

            item {
                SectionLabel("Machine")

                SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp)) {
                    Column(modifier = Modifier.fillMaxWidth().padding(16.dp)) {
                        val m = metrics

                        if (m == null) {
                            Text(metricsNote.ifEmpty { "reading…" }, style = MaterialTheme.typography.bodySmall, color = Muted)
                        } else {
                            MetricRow("load", String.format("%.2f", m.load1))
                            MetricRow("memory", "${m.memUsed} / ${m.memTotal} MB")
                            MetricRow("disk", "${m.disk}%")
                            MetricRow("uptime", uptimeText(m.uptime))
                            MetricRow("connections", "${m.conns}")

                            if (series.size > 1) {
                                Text(
                                    "memory · last ${series.size} samples",
                                    style = MaterialTheme.typography.labelMedium,
                                    color = Muted,
                                    modifier = Modifier.padding(top = 12.dp),
                                )
                                MetricBars(series.map { if (it.memTotal == 0) 0.0 else it.memUsed.toDouble() / it.memTotal })

                                Row(modifier = Modifier.fillMaxWidth().padding(top = 4.dp)) {
                                    Text(timeText(series.first().ts), style = MaterialTheme.typography.labelSmall, color = Muted)
                                    Spacer(Modifier.weight(1f))
                                    Text(timeText(series.last().ts), style = MaterialTheme.typography.labelSmall, color = Muted)
                                }
                            }

                            val age = (System.currentTimeMillis() / 1000) - m.ts

                            Text(
                                when {
                                    age > 1800 -> "stale: the last sample is ${age / 60} min old"
                                    else -> "as of ${timeText(m.ts)}"
                                },
                                style = MaterialTheme.typography.labelMedium,
                                color = if (age > 1800) Warn else Muted,
                                modifier = Modifier.padding(top = 8.dp),
                            )
                        }
                    }
                }
            }

            item {
                SectionLabel("Certificate")

                SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp)) {
                    Column(modifier = Modifier.fillMaxWidth().padding(16.dp)) {
                        if (certs.isEmpty()) {
                            Text(certNote.ifEmpty { "reading…" }, style = MaterialTheme.typography.bodySmall, color = Muted)
                        } else {
                            certs.forEach { cert ->
                                val days = (cert.notAfter - System.currentTimeMillis() / 1000) / 86400

                                Row(
                                    modifier = Modifier.fillMaxWidth().padding(vertical = 3.dp),
                                    verticalAlignment = Alignment.CenterVertically,
                                ) {
                                    MonoText(cert.name, color = Ink, style = MaterialTheme.typography.bodySmall, modifier = Modifier.weight(1f))
                                    Text(
                                        when {
                                            days < 0 -> "expired"
                                            days == 0L -> "today"
                                            else -> "in ${days} d"
                                        },
                                        style = MaterialTheme.typography.labelMedium,
                                        color = when {
                                            days < 0 -> DangerText
                                            days < 14 -> Warn
                                            else -> OkText
                                        },
                                    )
                                }

                                if (cert.names.isNotEmpty()) {
                                    Text(
                                        cert.names.joinToString(", "),
                                        style = MaterialTheme.typography.labelSmall,
                                        color = Muted,
                                    )
                                }
                            }
                        }
                    }
                }
            }

            item {
                SectionLabel(
                    if (h?.role == "entry") "Inbounds · what the entry serves" else "Inbounds · what the node serves",
                )

                SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp)) {
                    inbounds.forEachIndexed { index, inbound ->
                        if (index > 0) {
                            HorizontalDivider(color = Edge)
                        }

                        val count = users.count { user ->
                            when (inbound.name) {
                                "mtg" -> user.tg
                                else -> inbound.name in user.protocols
                            }
                        }

                        Row(
                            modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Column(modifier = Modifier.weight(1f)) {
                                Text(inbound.name, style = MaterialTheme.typography.titleSmall)
                                Text(
                                    "port ${inbound.port}" +
                                        if (h?.role == "entry" && count > 0) " · $count ${plural(count, "user", "users")}" else "",
                                    style = MaterialTheme.typography.bodySmall,
                                    color = Muted,
                                )
                            }

                            Switch(
                                checked = inbound.enabled,
                                enabled = toggling.isEmpty(),
                                onCheckedChange = { on ->
                                    toggling = inbound.name

                                    scope.launch {
                                        status = onIo { toggleInboundText(Core.current(), name, inbound.name, on) }
                                        toggling = ""

                                        if (status.startsWith("ok")) {
                                            epoch++
                                        }
                                    }
                                },
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

    if (editing) {
        AppDialog(
            title = "Edit $name",
            onDismiss = { editing = false },
            confirmLabel = if (editBusy) "saving…" else "Save",
            confirmEnabled = !editBusy,
            onConfirm = {
                editBusy = true

                scope.launch {
                    status = onIo {
                        editHostText(Core.current(), name, editSSH, editIP, editDomain, editVia, editEngine)
                    }
                    editBusy = false

                    if (status.startsWith("ok")) {
                        editing = false
                        epoch++
                    }
                }
            },
            content = {
                Text(
                    "the name, the role and the link secrets are not editable here: a rename or a role change rewrites both",
                    style = MaterialTheme.typography.bodySmall,
                    color = Muted,
                )

                OutlinedTextField(
                    value = editSSH,
                    onValueChange = { editSSH = it },
                    label = { Text("ssh, empty — leave it") },
                    singleLine = true,
                    textStyle = MaterialTheme.typography.bodyMedium,
                    colors = appFieldColors(),
                    modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
                )
                OutlinedTextField(
                    value = editIP,
                    onValueChange = { editIP = it },
                    label = { Text("ip") },
                    singleLine = true,
                    textStyle = MaterialTheme.typography.bodyMedium,
                    colors = appFieldColors(),
                    modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
                )
                OutlinedTextField(
                    value = editDomain,
                    onValueChange = { editDomain = it },
                    label = { Text("domain, entries only") },
                    singleLine = true,
                    textStyle = MaterialTheme.typography.bodyMedium,
                    colors = appFieldColors(),
                    modifier = Modifier.fillMaxWidth().padding(top = 8.dp),
                )

                Text("engine", style = MaterialTheme.typography.labelMedium, color = Muted, modifier = Modifier.padding(top = 10.dp, bottom = 4.dp))
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Chip("sing-box", { editEngine = "sing-box" }, selected = editEngine == "sing-box")
                    Chip("xray", { editEngine = "xray" }, selected = editEngine == "xray")
                }
            },
        )
    }
}

/** MetricsRow is the newest machine sample the host collected. */
data class MetricsRow(
    val ts: Long,
    val load1: Double,
    val memUsed: Int,
    val memTotal: Int,
    val disk: Int,
    val uptime: Long,
    val conns: Int,
)

/** InboundRow is one inbound of a host with its switch state. */
data class InboundRow(val name: String, val port: Int, val enabled: Boolean)

/** parseInbounds reads mobile.Inbounds. */
fun parseInbounds(json: String): List<InboundRow> = try {
    val arr = org.json.JSONArray(json)
    (0 until arr.length()).map { i ->
        val o = org.json.JSONObject(arr.getString(i))
        InboundRow(o.optString("name"), o.optInt("port"), o.optBoolean("enabled", true))
    }
} catch (e: Exception) {
    emptyList()
}

/** toggleInboundText flips one inbound; the change reaches the host on apply. */
fun toggleInboundText(app: mobile.App?, host: String, name: String, on: Boolean): String {
    if (app == null) {
        return "no state"
    }

    return try {
        "ok: ${app.toggleInbound(host, name, on)} · applied on the next apply"
    } catch (e: Exception) {
        "error: ${e.message}"
    }
}

/** CertRow is one certificate the host holds. */
data class CertRow(val name: String, val notAfter: Long, val names: List<String>)

/** parseCerts reads the agent's certificate list. */
fun parseCerts(json: String): List<CertRow> = try {
    val arr = org.json.JSONArray(json)
    (0 until arr.length()).map { i ->
        val o = org.json.JSONObject(arr.getString(i))
        val names = o.optJSONArray("names")

        CertRow(
            name = o.optString("name"),
            notAfter = o.optLong("not_after"),
            names = if (names == null) emptyList() else (0 until names.length()).map { names.getString(it) },
        )
    }
} catch (e: Exception) {
    emptyList()
}

/** parseMetricsSeries reads every sample the agent returned, oldest first. */
fun parseMetricsSeries(json: String): List<MetricsRow> = try {
    val arr = org.json.JSONArray(json)
    (0 until arr.length()).mapNotNull { i ->
        val o = org.json.JSONObject(arr.optString(i))
        if (o.optLong("ts") == 0L) {
            null
        } else {
            MetricsRow(
                ts = o.optLong("ts"),
                load1 = o.optDouble("load1", 0.0),
                memUsed = o.optInt("mem_used_mb"),
                memTotal = o.optInt("mem_total_mb"),
                disk = o.optInt("disk_pct"),
                uptime = o.optLong("uptime_s"),
                conns = o.optInt("conns"),
            )
        }
    }
} catch (e: Exception) {
    emptyList()
}

/** MetricBars draws memory use over the samples: one bar per five minutes. */
@Composable
fun MetricBars(values: List<Double>) {
    androidx.compose.foundation.Canvas(modifier = Modifier.fillMaxWidth().height(56.dp).padding(top = 8.dp)) {
        if (values.isEmpty()) {
            return@Canvas
        }

        val step = size.width / values.size

        values.forEachIndexed { index, value ->
            val high = value * 100 > 85
            val h = (value * size.height).toFloat().coerceAtLeast(1f)

            drawRect(
                color = if (high) Warn else Ok,
                topLeft = androidx.compose.ui.geometry.Offset(index * step, size.height - h),
                size = androidx.compose.ui.geometry.Size(step * 0.75f, h),
            )
        }
    }
}

/** uptimeText is days and hours, the shape an operator reads. */
fun uptimeText(seconds: Long): String {
    val days = seconds / 86400
    val hours = (seconds % 86400) / 3600

    return when {
        days > 0 -> "${days}d ${hours}h"
        hours > 0 -> "${hours}h"
        else -> "${seconds / 60}m"
    }
}

/** timeText is the sample time in the phone's zone. */
fun timeText(ts: Long): String =
    java.time.Instant.ofEpochSecond(ts).atZone(java.time.ZoneId.systemDefault())
        .format(java.time.format.DateTimeFormatter.ofPattern("dd.MM HH:mm"))

/** MetricRow is one line of the machine card. */
@Composable
fun MetricRow(label: String, value: String) {
    Row(modifier = Modifier.fillMaxWidth().padding(vertical = 2.dp)) {
        Text(label, style = MaterialTheme.typography.bodySmall, color = Muted, modifier = Modifier.width(96.dp))
        MonoText(value, color = Ink, style = MaterialTheme.typography.bodySmall)
    }
}

/** editHostText writes the editable fields; empty values mean "leave it". */
fun editHostText(
    app: mobile.App?,
    name: String,
    ssh: String,
    ip: String,
    domain: String,
    via: String,
    engine: String,
): String {
    if (app == null) {
        return "no state"
    }

    return try {
        "ok: ${app.editHost(name, ssh, ip, domain, via, engine)} · push when the forge is reachable"
    } catch (e: Exception) {
        "error: ${e.message}"
    }
}
