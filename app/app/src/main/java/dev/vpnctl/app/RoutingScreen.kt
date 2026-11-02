package dev.vpnctl.app

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
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
import org.json.JSONArray
import org.json.JSONObject

/** One routing rule as the screen shows it. */
data class RouteRuleRow(
    val title: String,
    val detail: String,
    val action: String,
    val note: String,
    val enabled: Boolean,
)

/** One chain of rules: all, split or full. */
data class RouteGroupRow(val name: String, val label: String, val rules: List<RouteRuleRow>)

/** The routing of one host. */
data class RouteHostRow(
    val name: String,
    val role: String,
    val exit: String,
    val dns: String,
    val groups: List<RouteGroupRow>,
)

/** parseRouting reads what mobile.Routing returns. */
fun parseRouting(json: String): List<RouteHostRow> = try {
    val arr = JSONArray(json)
    (0 until arr.length()).map { i ->
        val o = arr.getJSONObject(i)
        val groups = o.optJSONArray("groups")

        RouteHostRow(
            name = o.getString("name"),
            role = o.optString("role"),
            exit = o.optString("exit"),
            dns = o.optString("dns"),
            groups = if (groups == null) {
                emptyList()
            } else {
                (0 until groups.length()).map { g ->
                    val grp = groups.getJSONObject(g)
                    val rules = grp.optJSONArray("rules")

                    RouteGroupRow(
                        name = grp.getString("name"),
                        label = grp.getString("label"),
                        rules = if (rules == null) {
                            emptyList()
                        } else {
                            (0 until rules.length()).map { r ->
                                val rule = rules.getJSONObject(r)
                                RouteRuleRow(
                                    title = rule.getString("title"),
                                    detail = rule.optString("detail"),
                                    action = rule.getString("action"),
                                    note = rule.optString("note"),
                                    enabled = rule.optBoolean("enabled", true),
                                )
                            }
                        },
                    )
                }
            },
        )
    }
} catch (e: Exception) {
    emptyList()
}

/** One rule the tester consulted. */
data class RouteTraceRow(val rule: Int, val title: String, val verdict: String)

/** The tester's answer and the walk that produced it. */
data class RouteAnswerRow(val result: String, val trace: List<RouteTraceRow>)

/** parseRouteAnswer reads mobile.RouteTest. */
fun parseRouteAnswer(json: String): RouteAnswerRow = try {
    val o = JSONObject(json)
    val trace = o.optJSONArray("trace")

    RouteAnswerRow(
        result = o.optString("result"),
        trace = if (trace == null) {
            emptyList()
        } else {
            (0 until trace.length()).map { i ->
                val t = trace.getJSONObject(i)
                RouteTraceRow(t.optInt("rule"), t.optString("title"), t.optString("verdict"))
            }
        },
    )
} catch (e: Exception) {
    RouteAnswerRow("error reading the answer", emptyList())
}

/**
 * RoutingScreen is the rules of one host, chain by chain. The order is the
 * meaning — top-down, first match wins — so the list keeps it, a move is one
 * step, and a disabled rule stays in place. The tester answers only what the
 * phone holds: explicit domains; geosite and addresses stop the walk.
 */
@Composable
fun RoutingScreen(
    ctx: android.content.Context,
    modifier: Modifier,
    onBack: () -> Unit,
    onSets: () -> Unit,
) {
    val app = remember { Core.current() }
    val scope = rememberCoroutineScope()

    var hosts by remember { mutableStateOf(emptyList<RouteHostRow>()) }
    var picked by remember { mutableStateOf("") }
    var group by remember { mutableStateOf(0) }
    var status by remember { mutableStateOf("") }
    var epoch by remember { mutableStateOf(0) }

    var editing by remember { mutableStateOf<Int?>(null) }
    var ruleOn by remember { mutableStateOf(true) }
    var adding by remember { mutableStateOf(false) }
    var kind by remember { mutableStateOf("set") }
    var value by remember { mutableStateOf("") }
    var action by remember { mutableStateOf("direct") }
    var note by remember { mutableStateOf("") }
    var testDomain by remember { mutableStateOf("") }
    var answer by remember { mutableStateOf<RouteAnswerRow?>(null) }
    var busy by remember { mutableStateOf(false) }
    var editingExit by remember { mutableStateOf(false) }
    var exitPolicy by remember { mutableStateOf("auto") }
    var exitPrefer by remember { mutableStateOf("") }
    var exitBusy by remember { mutableStateOf(false) }

    LaunchedEffect(epoch) {
        status = onIo {
            hosts = app?.let { parseRouting(it.routing()) } ?: emptyList()

            if (hosts.none { it.name == picked }) {
                val entry = hosts.firstOrNull { it.role == "entry" } ?: hosts.firstOrNull()
                picked = entry?.name.orEmpty()
                group = entry?.groups?.indexOfFirst { it.name == "split" }?.takeIf { it >= 0 } ?: 0
            }

            ""
        }
    }

    val host = hosts.firstOrNull { it.name == picked }
    val active = host?.groups?.getOrNull(group)

    fun act(block: suspend () -> String) {
        if (busy) {
            return
        }

        busy = true

        scope.launch {
            status = onIo(block)
            busy = false

            if (status.startsWith("ok")) {
                epoch++
            }
        }
    }

    Column(modifier = modifier.fillMaxSize()) {
        ScreenHeader(
            title = "Routing",
            subtitle = "$picked · top-down, first match wins",
            onBack = onBack,
        )

        LazyRow(
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = 16.dp, vertical = 8.dp),
        ) {
            items(hosts.size) { index ->
                val h = hosts[index]
                Chip(
                    h.name,
                    onClick = {
                        picked = h.name
                        group = h.groups.indexOfFirst { it.name == "split" }.takeIf { it >= 0 } ?: 0
                        answer = null
                    },
                    selected = h.name == picked,
                )
            }
        }

        Row(modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp)) {
            if (!host?.exit.isNullOrEmpty()) {
                Box(modifier = Modifier.clickable {
                    exitPolicy = if (host.exit.startsWith("fixed")) "fixed" else "auto"
                    exitPrefer = host.exit.substringAfter("prefer ", "").trim()
                    editingExit = true
                }) {
                    InfoChip("exit · ${host.exit}", color = Muted)
                }
            }
        }

        Spacer(Modifier.height(8.dp))

        if (host != null) {
            UnderlineTabs(
                labels = host.groups.map { "${it.label} · ${it.rules.size}" },
                selected = group,
                onSelect = { group = it },
            )
        }

        LazyColumn(modifier = Modifier.weight(1f)) {
            item {
                if (active == null || active.rules.isEmpty()) {
                    Text(
                        "no rules in this chain",
                        style = MaterialTheme.typography.bodySmall,
                        color = Muted,
                        modifier = Modifier.padding(16.dp),
                    )
                } else {
                    SurfaceCard(modifier = Modifier.padding(16.dp)) {
                        active.rules.forEachIndexed { index, rule ->
                            if (index > 0) {
                                HorizontalDivider(color = Edge)
                            }

                            RouteRuleView(index + 1, rule) {
                                ruleOn = rule.enabled
                                editing = index
                            }
                        }

                        HorizontalDivider(color = Edge)
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .clickable {
                                    kind = "set"
                                    value = ""
                                    action = "direct"
                                    note = ""
                                    adding = true
                                }
                                .padding(horizontal = 16.dp, vertical = 12.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Text("+ Rule", style = MaterialTheme.typography.bodyMedium, color = Link)
                        }
                    }
                }
            }

            item {
                SectionLabel("Where does it go?")

                SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp)) {
                    Column(modifier = Modifier.fillMaxWidth().padding(16.dp)) {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            OutlinedTextField(
                                value = testDomain,
                                onValueChange = { testDomain = it },
                                label = { Text("domain") },
                                singleLine = true,
                                textStyle = MaterialTheme.typography.bodyMedium,
                                colors = appFieldColors(),
                                modifier = Modifier.weight(1f),
                            )
                            Spacer(Modifier.width(8.dp))
                            TextButton(
                                onClick = {
                                    val json = app?.routeTest(picked, active?.name ?: "split", testDomain) ?: "{}"
                                    answer = parseRouteAnswer(json)
                                },
                                enabled = testDomain.isNotBlank(),
                            ) {
                                Text("Check", color = Link)
                            }
                        }

                        answer?.let { a ->
                            Text(
                                "→ ${a.result}",
                                style = MaterialTheme.typography.titleSmall,
                                color = if (a.result == "direct" || a.result.startsWith("→ link")) OkText else Warn,
                                modifier = Modifier.padding(top = 8.dp),
                            )
                            a.trace.forEach { t ->
                                Text(
                                    "${t.rule} · ${t.title} — ${t.verdict}",
                                    style = monoStyle(MaterialTheme.typography.labelMedium),
                                    color = if (t.verdict == "matched") OkText else Muted,
                                    modifier = Modifier.padding(top = 2.dp),
                                )
                            }
                        }

                        Text(
                            "the phone answers for explicit domains; geosite and addresses are decided on the host",
                            style = MaterialTheme.typography.labelMedium,
                            color = Muted,
                            modifier = Modifier.padding(top = 8.dp),
                        )
                    }
                }
            }

            item {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clickable(onClick = onSets)
                        .padding(horizontal = 16.dp, vertical = 12.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text("Address sets", style = MaterialTheme.typography.bodyMedium, color = Link)
                    Spacer(Modifier.weight(1f))
                    Text("domains and IPs the rules match", style = MaterialTheme.typography.labelMedium, color = Muted)
                }
            }

            item {
                Text(
                    "the order is the routing · rules are applied on the hosts",
                    style = MaterialTheme.typography.labelMedium,
                    color = Muted,
                    modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                )
            }

            if (status.isNotEmpty()) {
                item {
                    Text(
                        status,
                        style = MaterialTheme.typography.bodySmall,
                        color = if (status.startsWith("error")) DangerText else Muted,
                        modifier = Modifier.padding(horizontal = 16.dp),
                    )
                }
            }

            item { Spacer(Modifier.height(24.dp)) }
        }
    }

    val chain = active?.name ?: "split"

    if (editingExit) {
        val exits = hosts.filter { it.role != "entry" }

        AppDialog(
            title = "Exit for $picked",
            onDismiss = { editingExit = false },
            confirmLabel = "Save",
            confirmEnabled = !exitBusy && (exitPolicy == "auto" || exitPrefer.isNotEmpty()),
            onConfirm = {
                exitBusy = true

                scope.launch {
                    status = onIo { setExitText(Core.current(), picked, exitPolicy, exitPrefer) }
                    exitBusy = false

                    if (status.startsWith("ok")) {
                        editingExit = false
                        epoch++
                    }
                }
            },
            content = {
                Text(
                    "auto lets the engine pick the fastest exit; fixed pins every connection to one",
                    style = MaterialTheme.typography.bodySmall,
                    color = Muted,
                )

                Row(modifier = Modifier.padding(top = 10.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Chip("auto", { exitPolicy = "auto" }, selected = exitPolicy == "auto")
                    Chip("fixed", { exitPolicy = "fixed" }, selected = exitPolicy == "fixed")
                }

                if (exitPolicy == "fixed") {
                    Text("exit", style = MaterialTheme.typography.labelMedium, color = Muted, modifier = Modifier.padding(top = 10.dp, bottom = 4.dp))
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        exits.forEach { e ->
                            Chip(e.name, { exitPrefer = e.name }, selected = exitPrefer == e.name)
                        }
                    }
                }
            },
        )
    }

    editing?.let { index ->
        val rule = active?.rules?.getOrNull(index)

        if (rule != null) {
            AppDialog(
                title = "Rule ${index + 1} · ${rule.title}",
                onDismiss = { editing = null },
                confirmLabel = "Save",
                confirmEnabled = !busy && ruleOn != rule.enabled,
                onConfirm = {
                    editing = null

                    act {
                        ruleToggleText(Core.current(), picked, chain, index, ruleOn)
                    }
                },
                dismissLabel = "Close",
                content = {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Column(modifier = Modifier.weight(1f)) {
                            Text(rule.action, style = MaterialTheme.typography.titleSmall)
                            if (rule.note.isNotEmpty()) {
                                Text(rule.note, style = MaterialTheme.typography.bodySmall, color = Muted)
                            }
                        }

                        Switch(
                            checked = ruleOn,
                            onCheckedChange = { ruleOn = it },
                            colors = SwitchDefaults.colors(
                                checkedThumbColor = Color.White,
                                checkedTrackColor = Accent,
                                uncheckedThumbColor = Muted,
                                uncheckedTrackColor = Surface2,
                                uncheckedBorderColor = Edge,
                            ),
                        )
                    }

                    Row(modifier = Modifier.padding(top = 8.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        TextButton(
                            onClick = {
                                editing = null
                                act { ruleMoveText(Core.current(), picked, chain, index, true) }
                            },
                            enabled = !busy && index > 0,
                        ) { Text("Move up", color = Link, style = MaterialTheme.typography.bodySmall) }

                        TextButton(
                            onClick = {
                                editing = null
                                act { ruleMoveText(Core.current(), picked, chain, index, false) }
                            },
                            enabled = !busy && index < (active?.rules?.size ?: 0) - 1,
                        ) { Text("Move down", color = Link, style = MaterialTheme.typography.bodySmall) }

                        TextButton(
                            onClick = {
                                editing = null
                                act { ruleRemoveText(Core.current(), picked, chain, index) }
                            },
                            enabled = !busy,
                        ) { Text("Delete", color = DangerText, style = MaterialTheme.typography.bodySmall) }
                    }
                },
            )
        }
    }

    if (adding) {
        AppDialog(
            title = "New rule",
            onDismiss = { adding = false },
            confirmLabel = "Add",
            confirmEnabled = !busy && value.isNotBlank(),
            onConfirm = {
                adding = false
                act { ruleAddText(Core.current(), picked, chain, kind, value.trim(), action, note.trim()) }
            },
            content = {
                Text(
                    "the rule goes to the end of the chain: a new rule never takes over a connection the operator already routed",
                    style = MaterialTheme.typography.bodySmall,
                    color = Muted,
                )

                Text("match", style = MaterialTheme.typography.labelMedium, color = Muted, modifier = Modifier.padding(top = 10.dp, bottom = 4.dp))
                Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    listOf("set", "geoip", "inbound", "domains").chunked(2).forEach { pair ->
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            pair.forEach { k ->
                                Chip(k, { kind = k }, selected = kind == k)
                            }
                        }
                    }
                }

                OutlinedTextField(
                    value = value,
                    onValueChange = { value = it },
                    label = {
                        Text(
                            when (kind) {
                                "set" -> "set name, e.g. ru-direct"
                                "geoip" -> "geoip codes, e.g. ru,private"
                                "inbound" -> "inbound, e.g. probe"
                                else -> "domains, comma separated"
                            },
                        )
                    },
                    singleLine = true,
                    textStyle = MaterialTheme.typography.bodyMedium,
                    colors = appFieldColors(),
                    modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
                )

                Text("action", style = MaterialTheme.typography.labelMedium, color = Muted, modifier = Modifier.padding(top = 10.dp, bottom = 4.dp))
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    listOf("direct", "block", "link:auto").forEach { a ->
                        Chip(a, { action = a }, selected = action == a)
                    }
                }

                OutlinedTextField(
                    value = note,
                    onValueChange = { note = it },
                    label = { Text("note, optional") },
                    singleLine = true,
                    textStyle = MaterialTheme.typography.bodyMedium,
                    colors = appFieldColors(),
                    modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
                )
            },
        )
    }
}

/** RouteRuleView is one rule: its number, what it matches, and what it does. */
@Composable
fun RouteRuleView(number: Int, rule: RouteRuleRow, onClick: () -> Unit = {}) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(horizontal = 16.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            "$number",
            style = monoStyle(MaterialTheme.typography.bodySmall),
            color = if (rule.enabled) Muted else DangerText,
            modifier = Modifier.width(22.dp),
        )

        Column(modifier = Modifier.weight(1f).padding(horizontal = 8.dp)) {
            Text(rule.title, style = MaterialTheme.typography.titleSmall, maxLines = 2)
            if (rule.detail.isNotEmpty()) {
                Text(
                    rule.detail,
                    style = MaterialTheme.typography.bodySmall,
                    color = Muted,
                    modifier = Modifier.padding(top = 2.dp),
                )
            }
            if (rule.note.isNotEmpty()) {
                Text(
                    rule.note,
                    style = MaterialTheme.typography.labelMedium,
                    color = Muted,
                    modifier = Modifier.padding(top = 4.dp),
                )
            }
        }

        val (bg, fg) = when {
            rule.action == "direct" -> OkSoft to OkText
            rule.action == "block" -> DangerSoft to DangerText
            else -> AccentSoft to OnAccentSoft
        }

        Badge(if (rule.enabled) rule.action else "off", bg, fg)
    }
}

// ── the mutating calls ─────────────────────────────────────────────────────

/** setExitText pins or frees the exit of one host. */
fun setExitText(app: mobile.App?, host: String, policy: String, prefer: String): String {
    if (app == null) {
        return "no state"
    }

    return try {
        "ok: ${app.setExit(host, policy, prefer)} · applied on the next apply"
    } catch (e: Exception) {
        "error: ${e.message}"
    }
}

fun ruleAddText(app: mobile.App?, host: String, chain: String, kind: String, value: String, action: String, note: String): String {
    if (app == null) {
        return "no state"
    }

    return wrapper {
        when (kind) {
            "set" -> app.addRule(host, chain, value, "", "", "", "", action, note)
            "geoip" -> app.addRule(host, chain, "", value, "", "", "", action, note)
            "inbound" -> app.addRule(host, chain, "", "", value, "", "", action, note)
            else -> app.addRule(host, chain, "", "", "", value, "", action, note)
        }
    }
}

fun ruleRemoveText(app: mobile.App?, host: String, chain: String, index: Int): String {
    if (app == null) {
        return "no state"
    }

    return wrapper { app.removeRule(host, chain, index.toLong()) }
}

fun ruleMoveText(app: mobile.App?, host: String, chain: String, index: Int, up: Boolean): String {
    if (app == null) {
        return "no state"
    }

    return wrapper { app.moveRule(host, chain, index.toLong(), up) }
}

fun ruleToggleText(app: mobile.App?, host: String, chain: String, index: Int, on: Boolean): String {
    if (app == null) {
        return "no state"
    }

    return wrapper { app.toggleRule(host, chain, index.toLong(), on) }
}

private fun wrapper(call: () -> String): String = try {
    "ok: ${call()}"
} catch (e: Exception) {
    "error: ${e.message}"
}
