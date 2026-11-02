package dev.vpnctl.app

import androidx.compose.foundation.layout.Arrangement
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
import org.json.JSONArray

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

/**
 * RoutingScreen is the rules of one host, chain by chain. The order is the
 * meaning — top-down, first match wins — so the list keeps it and says so.
 * Address lookup is not here: geosite and geoip live on the hosts.
 */
@Composable
fun RoutingScreen(ctx: android.content.Context, modifier: Modifier, onBack: () -> Unit) {
    val app = remember { Core.current() }
    var hosts by remember { mutableStateOf(emptyList<RouteHostRow>()) }
    var picked by remember { mutableStateOf("") }
    var group by remember { mutableStateOf(0) }
    var status by remember { mutableStateOf("") }

    LaunchedEffect(Unit) {
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
                    },
                    selected = h.name == picked,
                )
            }
        }

        Row(modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp)) {
            if (!host?.exit.isNullOrEmpty()) {
                InfoChip("exit · ${host.exit}", color = Muted)
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

                            RouteRuleView(index + 1, rule)
                        }
                    }
                }
            }

            item {
                Text(
                    "the order is the routing · rules are applied on the hosts, editing stays in the state file",
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
                        color = DangerText,
                        modifier = Modifier.padding(horizontal = 16.dp),
                    )
                }
            }

            item { Spacer(Modifier.height(24.dp)) }
        }
    }
}

/** RouteRuleView is one rule: its number, what it matches, and what it does. */
@Composable
fun RouteRuleView(number: Int, rule: RouteRuleRow) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 14.dp),
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
