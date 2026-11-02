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
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
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
    var repo by remember { mutableStateOf(RepoStateRow()) }
    var drift by remember { mutableStateOf<Pair<String, String>?>(null) }
    var diffText by remember { mutableStateOf("") }
    var diffErr by remember { mutableStateOf("") }
    var applying by remember { mutableStateOf(false) }
    var applyLines by remember { mutableStateOf(emptyList<String>()) }
    var applyDone by remember { mutableStateOf(false) }

    val head = remember { app?.head().orEmpty() }
    val files = hosts.sumOf { h -> h.files.count { it.status != "=" } }
    val changed = hosts.count { it.changes > 0 || it.unreachable.isNotEmpty() }

    suspend fun load() {
        probing = true
        repo = withContext(Dispatchers.IO) { parseRepoState(app?.repoState() ?: "{}") }
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

    if (applying) {
        ApplyScreen(
            lines = applyLines,
            done = applyDone,
            busy = busy,
            expected = hosts.map { it.name },
            onClose = {
                applying = false
                applyLines = emptyList()
            },
        )

        return
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
                        if (repo.branch.isNotEmpty()) {
                            val ready = repo.clean && repo.pushed
                            val what = when {
                                !repo.clean -> "uncommitted changes — commit or stash first"
                                !repo.pushed -> "not pushed — push before apply"
                                else -> "clean tree · pushed"
                            }

                            Text(
                                (if (ready) "✓ " else "! ") + "${repo.branch} · $what",
                                style = MaterialTheme.typography.bodySmall,
                                color = if (ready) OkText else Warn,
                            )
                        }

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
                HostPlanCard(
                    number = index + 1,
                    host = host,
                    role = roles[host.name].orEmpty(),
                    head = head,
                    onDiff = { path ->
                        drift = host.name to path
                        diffText = ""
                        diffErr = "reading the host…"

                        scope.launch {
                            val out = onIo { app?.diff(host.name, path) ?: "no state" }

                            if (out.startsWith("error")) {
                                diffErr = out
                            } else {
                                diffText = out
                                diffErr = ""
                            }
                        }
                    },
                    onAdopt = { path ->
                        act {
                            val out = onIo { app?.adopt(host.name, path)?.let { "ok: $it" } ?: "no state" }
                            if (out.startsWith("ok")) {
                                drift = null
                                load()
                            }

                            out
                        }
                    },
                )
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
                if (repo.branch.isNotEmpty() && (!repo.clean || !repo.pushed)) {
                    "the state must be clean and on origin before the hosts move"
                } else {
                    "exits first, the jump host last · stop and roll back if the probe fails"
                },
                style = MaterialTheme.typography.labelMedium,
                color = Muted,
                modifier = Modifier.padding(bottom = 10.dp),
            )

            Row(verticalAlignment = Alignment.CenterVertically) {
                val canApply = repo.branch.isNotEmpty() && repo.clean && repo.pushed

                PrimaryButton(
                    text = when {
                        busy -> if (canApply) "applying…" else "pushing…"
                        canApply -> "Apply ${hosts.size} hosts"
                        else -> "Push to origin"
                    },
                    onClick = {
                        if (busy) {
                            return@PrimaryButton
                        }

                        if (!canApply) {
                            busy = true
                            status = "pushing…"

                            scope.launch {
                                val out = try {
                                    onIo { pushState(app) }
                                } catch (e: Exception) {
                                    "error: ${e.message}"
                                }

                                busy = false
                                status = out

                                if (out.startsWith("pushed")) {
                                    load()
                                }
                            }

                            return@PrimaryButton
                        }

                        applying = true
                        applyDone = false
                        applyLines = emptyList()
                        busy = true
                        status = "applying…"

                        scope.launch {
                            val out = onIo {
                                applyWithProgress(app, "") { line -> applyLines = applyLines + line }
                            }

                            busy = false
                            applyDone = true
                            status = out

                            // the hosts just changed: show what is left, not what was
                            if (out == "applied") {
                                load()
                            }
                        }
                    },
                    enabled = !busy && hosts.isNotEmpty(),
                    modifier = Modifier.weight(1f),
                )
            }
        }
    }

    drift?.let { (host, path) ->
        AppDialog(
            title = "Drift · $path",
            onDismiss = { drift = null },
            confirmLabel = "Adopt into repo",
            confirmEnabled = !busy && diffText.isNotEmpty(),
            onConfirm = {
                act {
                    val out = onIo { app?.adopt(host, path)?.let { "ok: $it" } ?: "no state" }
                    if (out.startsWith("ok")) {
                        drift = null
                        load()
                    }

                    out
                }
            },
            dismissLabel = "Close",
            content = {
                Text(
                    "the host's copy against what the render wants; adopting keeps the host's version",
                    style = MaterialTheme.typography.bodySmall,
                    color = Muted,
                )

                if (diffErr.isNotEmpty()) {
                    Text(diffErr, style = MaterialTheme.typography.bodySmall, color = DangerText, modifier = Modifier.padding(top = 8.dp))
                }

                if (diffText.isNotEmpty()) {
                    androidx.compose.foundation.layout.Box(
                        modifier = Modifier
                            .fillMaxWidth()
                            .height(300.dp)
                            .padding(top = 8.dp)
                            .clip(RoundedCornerShape(12.dp))
                            .background(Canvas)
                            .verticalScroll(rememberScrollState())
                            .padding(12.dp),
                    ) {
                        Text(diffText, style = monoStyle(MaterialTheme.typography.labelMedium), color = Ink)
                    }
                }
            },
        )
    }
}

/** One host of a running apply: its name, the lines it printed, its state. */
data class ApplySection(val name: String, val lines: List<String>, val state: String)

/**
 * parseApply groups the apply output by host. The lines are the command line's
 * own: "▸ host" opens a section, a line with ✗ marks it failed, and everything
 * after the next "▸" or the final "done" belongs to the host above.
 */
fun parseApply(lines: List<String>, done: Boolean, expected: List<String> = emptyList()): List<ApplySection> {
    val bodies = mutableMapOf<String, MutableList<String>>()
    val order = mutableListOf<String>()

    // the series names its own order in the first line; until that line arrives
    // the screen already knows which hosts the plan covers
    val preamble = lines.firstOrNull { it.startsWith("plan for ") }.orEmpty()
    val declared = preamble.substringAfter("order: ", "").split(" → ").map { it.trim() }.filter { it.isNotEmpty() }

    (if (declared.isNotEmpty()) declared else expected).forEach {
        order += it
        bodies[it] = mutableListOf()
    }

    var current = ""

    for (line in lines) {
        when {
            line.startsWith("▸ ") -> {
                current = line.removePrefix("▸ ").trim()

                if (current !in bodies) {
                    order += current
                    bodies[current] = mutableListOf()
                }
            }
            current.isNotEmpty() -> bodies.getValue(current).add(line)
        }
    }

    return order.mapIndexed { index, name ->
        val body = bodies[name].orEmpty()
        val failed = body.any { it.contains("✗") }

        val state = when {
            failed -> "failed"
            body.isEmpty() && !done -> "pending"
            body.isEmpty() && done -> "skipped"
            index < order.lastIndex || done -> "done"
            else -> "running"
        }

        ApplySection(name, body, state)
    }
}

/** ApplyScreen is the mockup's apply-in-progress: one block per host, in order. */
@Composable
fun ApplyScreen(
    lines: List<String>,
    done: Boolean,
    busy: Boolean,
    expected: List<String>,
    onClose: () -> Unit,
) {
    val sections = parseApply(lines, done, expected)
    val preamble = lines.firstOrNull { it.startsWith("plan for ") }.orEmpty()
    val failed = sections.any { it.state == "failed" }

    Column(modifier = Modifier.fillMaxSize()) {
        ScreenHeader(
            title = if (done) "Apply · ${if (failed) "failed" else "done"}" else "Apply",
            subtitle = "${sections.size} ${plural(sections.size, "host", "hosts")}",
            onBack = onClose,
            backIcon = Icons.Outlined.Close,
        )

        LazyColumn(modifier = Modifier.weight(1f)) {
            if (preamble.isNotEmpty()) {
                item {
                    Text(
                        preamble,
                        style = monoStyle(MaterialTheme.typography.labelMedium),
                        color = Muted,
                        modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                    )
                }
            }

            itemsIndexed(sections) { index, section ->
                SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp, vertical = 6.dp)) {
                    Row(
                        modifier = Modifier.fillMaxWidth().padding(16.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        StatusDot(
                            when (section.state) {
                                "failed" -> Danger
                                "done" -> Ok
                                "pending", "skipped" -> Muted
                                else -> Warn
                            },
                            size = 10.dp,
                        )
                        Text(
                            section.name,
                            style = MaterialTheme.typography.titleLarge,
                            modifier = Modifier.padding(start = 12.dp).weight(1f),
                        )
                        Text(
                            section.state,
                            style = MaterialTheme.typography.labelMedium,
                            color = when (section.state) {
                                "failed" -> DangerText
                                "done" -> OkText
                                "pending", "skipped" -> Muted
                                else -> Warn
                            },
                        )
                    }

                    section.lines.forEach { line ->
                        Text(
                            line,
                            style = monoStyle(MaterialTheme.typography.labelMedium),
                            color = when {
                                line.contains("✗") -> DangerText
                                line.contains("✓") -> OkText
                                line.contains("!") -> Warn
                                else -> Muted
                            },
                            modifier = Modifier.padding(horizontal = 16.dp, vertical = 1.dp),
                        )
                    }

                    Spacer(Modifier.height(12.dp))
                }
            }

            item { Spacer(Modifier.height(16.dp)) }
        }

        Column(modifier = Modifier.fillMaxWidth().padding(16.dp)) {
            Text(
                if (done) "the series finished · the plan now shows what is left" else "applying · one host at a time, exits first",
                style = MaterialTheme.typography.labelMedium,
                color = Muted,
                modifier = Modifier.padding(bottom = 10.dp),
            )
            PrimaryButton(
                text = if (done) "Back to plan" else "applying…",
                onClick = onClose,
                enabled = done && !busy,
                modifier = Modifier.fillMaxWidth(),
            )
        }
    }
}

/** HostPlanCard is one numbered host: what changes, and what the probe said. */
@Composable
fun HostPlanCard(
    number: Int,
    host: PlanHost,
    role: String,
    head: String,
    onDiff: (String) -> Unit,
    onAdopt: (String) -> Unit,
) {
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

        host.files.filter { it.status == "!" }.forEach { file ->
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(start = 16.dp, end = 16.dp, top = 8.dp)
                    .clip(RoundedCornerShape(12.dp))
                    .background(WarnSoft)
                    .padding(12.dp),
            ) {
                Text("Drift · ${file.path}", style = MaterialTheme.typography.titleSmall, color = Warn)
                Text(
                    "edited on the host by hand; the next apply overwrites it",
                    style = MaterialTheme.typography.bodySmall,
                    color = Ink,
                    modifier = Modifier.padding(top = 2.dp),
                )
                Row {
                    TextButton(onClick = { onDiff(file.path) }) {
                        Text("Show diff", color = Warn, style = MaterialTheme.typography.bodySmall)
                    }
                    TextButton(onClick = { onAdopt(file.path) }) {
                        Text("Adopt into repo", color = Warn, style = MaterialTheme.typography.bodySmall)
                    }
                }
            }
        }

        host.files.filter { it.status != "=" && it.status != "!" }.forEach { file ->
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
