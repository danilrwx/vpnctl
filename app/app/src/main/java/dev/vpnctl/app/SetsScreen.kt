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
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
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
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.json.JSONArray

/** SetRow is one address set: the geodata names plus the operator's own entries. */
data class SetRow(
    val name: String,
    val geosite: List<String>,
    val geoip: List<String>,
    val domains: List<String>,
    val ips: List<String>,
)

/** parseSets reads mobile.Sets. */
fun parseSets(json: String): List<SetRow> = try {
    val arr = JSONArray(json)
    (0 until arr.length()).map { i ->
        val o = arr.getJSONObject(i)

        fun list(key: String): List<String> {
            val a = o.optJSONArray(key) ?: return emptyList()
            return (0 until a.length()).map { a.getString(it) }
        }

        SetRow(o.getString("name"), list("geosite"), list("geoip"), list("domains"), list("ips"))
    }
} catch (e: Exception) {
    emptyList()
}

/**
 * SetsScreen edits the operator's own domains and addresses of an address set.
 * The geosite and geoip names stay: they come from the pinned geodata, not from
 * the phone, and a set without its category is a different rule.
 */
@Composable
fun SetsScreen(ctx: android.content.Context, modifier: Modifier, onBack: () -> Unit) {
    val app = remember { Core.current() }
    val scope = rememberCoroutineScope()

    var sets by remember { mutableStateOf(emptyList<SetRow>()) }
    var status by remember { mutableStateOf("") }
    var editing by remember { mutableStateOf<SetRow?>(null) }
    var domains by remember { mutableStateOf("") }
    var ips by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var epoch by remember { mutableStateOf(0) }

    LaunchedEffect(epoch) {
        sets = withContext(Dispatchers.IO) { app?.let { parseSets(it.sets()) } ?: emptyList() }
    }

    Column(modifier = modifier.fillMaxSize()) {
        ScreenHeader(title = "Address sets", subtitle = "${sets.size} sets", onBack = onBack)

        LazyColumn(modifier = Modifier.weight(1f)) {
            item {
                Text(
                    "the rules match against these; the geosite and geoip names come from the pinned data",
                    style = MaterialTheme.typography.bodySmall,
                    color = Muted,
                    modifier = Modifier.padding(16.dp),
                )
            }

            item {
                SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp)) {
                    sets.forEachIndexed { index, set ->
                        if (index > 0) {
                            HorizontalDivider(color = Edge)
                        }

                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .clickable {
                                    domains = set.domains.joinToString("\n")
                                    ips = set.ips.joinToString("\n")
                                    editing = set
                                }
                                .padding(horizontal = 16.dp, vertical = 14.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Column(modifier = Modifier.weight(1f)) {
                                Text(set.name, style = MaterialTheme.typography.titleSmall)
                                Text(
                                    summary(set),
                                    style = MaterialTheme.typography.bodySmall,
                                    color = Muted,
                                    modifier = Modifier.padding(top = 2.dp),
                                )
                            }
                            Text("Edit", color = Link, style = MaterialTheme.typography.bodySmall)
                        }
                    }
                }
            }

            item {
                if (status.isNotEmpty()) {
                    Text(
                        status,
                        style = MaterialTheme.typography.bodySmall,
                        color = if (status.startsWith("error")) DangerText else Muted,
                        modifier = Modifier.padding(16.dp),
                    )
                }
            }

            item { Spacer(Modifier.height(24.dp)) }
        }
    }

    editing?.let { set ->
        AppDialog(
            title = set.name,
            onDismiss = { editing = null },
            confirmLabel = "Save",
            confirmEnabled = !busy,
            onConfirm = {
                busy = true

                scope.launch {
                    status = onIo {
                        saveSetText(
                            Core.current(),
                            set.name,
                            domains.split("\n").joinToString(",") { it.trim() },
                            ips.split("\n").joinToString(",") { it.trim() },
                        )
                    }
                    busy = false

                    if (status.startsWith("ok")) {
                        editing = null
                        epoch++
                    }
                }
            },
            content = {
                Text(
                    "one domain or address per line; the geosite and geoip names of the set are kept",
                    style = MaterialTheme.typography.bodySmall,
                    color = Muted,
                )

                OutlinedTextField(
                    value = domains,
                    onValueChange = { domains = it },
                    label = { Text("domains, one per line") },
                    minLines = 3,
                    maxLines = 8,
                    textStyle = MaterialTheme.typography.bodyMedium,
                    colors = appFieldColors(),
                    modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
                )
                OutlinedTextField(
                    value = ips,
                    onValueChange = { ips = it },
                    label = { Text("addresses, one per line") },
                    minLines = 2,
                    maxLines = 6,
                    textStyle = MaterialTheme.typography.bodyMedium,
                    colors = appFieldColors(),
                    modifier = Modifier.fillMaxWidth().padding(top = 8.dp),
                )
            },
        )
    }
}

/** summary is the one line under a set: what is in it and where it came from. */
fun summary(set: SetRow): String {
    val parts = mutableListOf<String>()

    if (set.geosite.isNotEmpty()) {
        parts += "geosite " + set.geosite.joinToString(",")
    }

    if (set.geoip.isNotEmpty()) {
        parts += "geoip " + set.geoip.joinToString(",")
    }

    parts += "${set.domains.size} domains"
    parts += "${set.ips.size} addresses"

    return parts.joinToString(" · ")
}

/** saveSetText writes the operator's own entries of a set. */
fun saveSetText(app: mobile.App?, name: String, domains: String, ips: String): String {
    if (app == null) {
        return "no state"
    }

    return try {
        "ok: ${app.saveSet(name, domains, ips)} · applied on the next apply"
    } catch (e: Exception) {
        "error: ${e.message}"
    }
}
