package dev.vpnctl.app

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.json.JSONArray
import java.io.File

/** One row of the list screen. */
data class UserRow(val name: String, val modes: String, val enabled: Boolean, val expires: String)

/** One copyable line of a user card. */
data class UserLink(val label: String, val url: String)

/** One host of the plan. */
data class PlanHost(val name: String, val changes: Int, val unreachable: String)

/** parsePlan reads what plan.JSONOf writes: a host, a change count, files. */
fun parsePlan(json: String): List<PlanHost> = try {
    val arr = JSONArray(json)
    (0 until arr.length()).map { i ->
        val o = arr.getJSONObject(i)
        PlanHost(
            name = o.getString("name"),
            changes = o.getInt("changes"),
            unreachable = o.optString("unreachable"),
        )
    }
} catch (e: Exception) {
    emptyList()
}

/** Users parses the JSON the core returns: a struct with a slice field cannot
 * cross the gomobile boundary, so every list arrives as text. */
fun parseUsers(json: String): List<UserRow> = try {
    val arr = JSONArray(json)
    (0 until arr.length()).map { i ->
        val o = arr.getJSONObject(i)
        val modes = o.getJSONArray("modes")
        UserRow(
            name = o.getString("name"),
            modes = (0 until modes.length()).joinToString("/") { modes.getString(it) },
            enabled = o.getBoolean("enabled"),
            expires = o.optString("expires"),
        )
    }
} catch (e: Exception) {
    emptyList()
}

/** parseLinks is the same shape for Links(): a label and a URL per row. */
fun parseLinks(json: String): List<UserLink> = try {
    val arr = JSONArray(json)
    (0 until arr.length()).map { i ->
        val o = arr.getJSONObject(i)
        UserLink(label = o.getString("label"), url = o.getString("url"))
    }
} catch (e: Exception) {
    emptyList()
}

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        Core.installSigner(applicationContext)
        setContent {
            MaterialTheme {
                Scaffold { padding ->
                    App(modifier = Modifier.padding(padding))
                }
            }
        }
    }
}

@Composable
fun App(modifier: Modifier = Modifier) {
    val ctx = androidx.compose.ui.platform.LocalContext.current
    var selected by remember { mutableStateOf<String?>(null) }
    // a new user is written by another screen; the list reads it again when this
    // changes, so the count and the rows cannot go stale
    var version by remember { mutableStateOf(0) }

    when (selected) {
        null -> UsersScreen(ctx, modifier, version, onPick = { selected = it })
        PLAN -> PlanScreen(ctx, modifier, onBack = { selected = null })
        ADD -> AddUserScreen(
            ctx,
            modifier,
            onBack = { selected = null },
            onCreated = {
                version++
                selected = null
            },
        )
        else -> UserScreen(ctx, modifier, name = selected!!, onBack = { selected = null })
    }
}

@Composable
fun UsersScreen(ctx: android.content.Context, modifier: Modifier, version: Int, onPick: (String) -> Unit) {
    var status by remember { mutableStateOf("") }

    LaunchedEffect(Unit) {
        if (Core.current() == null) {
            status = withContext(Dispatchers.IO) { Core.openBundled(ctx)?.let { "" } ?: "нет состояния" }
        }

        if (Core.current() != null && !File(ctx.filesDir, "state/.git").exists()) {
            status = withContext(Dispatchers.IO) { Core.sync(ctx, REPO) }
        }
    }

    // The handle is read here, not kept in remember: a Go object parked in Compose
    // state did not survive the sync (the write never reached the read), and the
    // list stayed on the APK fixture until the process restarted.
    val app = Core.current()
    val users = remember(app, version) { app?.let { parseUsers(it.users()) } ?: emptyList() }

    Column(modifier = modifier.fillMaxSize().padding(16.dp)) {
        if (status.isNotEmpty()) {
            Text(status, style = MaterialTheme.typography.bodySmall)
        }

        Text("users: ${users.size}", style = MaterialTheme.typography.titleMedium)
        Row {
            Button(onClick = { onPick(PLAN) }) { Text("план") }
            Button(onClick = { onPick(ADD) }, modifier = Modifier.padding(start = 8.dp)) { Text("новый") }
        }

        LazyColumn {
            items(users) { user ->
                val mark = if (user.enabled) "" else " · off"
                val end = if (user.expires.isEmpty()) "" else "  until ${user.expires}"
                Text(
                    "${user.name}$mark  ${user.modes}$end",
                    modifier = Modifier.fillMaxWidth().clickable { onPick(user.name) }.padding(vertical = 6.dp),
                )
            }
        }
    }
}

@Composable
fun UserScreen(ctx: android.content.Context, modifier: Modifier, name: String, onBack: () -> Unit) {
    val app = remember { Core.current() }
    var status by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var links by remember { mutableStateOf(emptyList<UserLink>()) }
    var message by remember { mutableStateOf("") }

    val scope = rememberCoroutineScope()

    // One action at a time: two applies over the same hosts would race, and a
    // disabled button is the only feedback a call this long has.
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

    LaunchedEffect(name) {
        links = withContext(Dispatchers.IO) {
            app?.let { parseLinks(it.links(name)) } ?: emptyList()
        }
    }

    Column(modifier = modifier.fillMaxSize().padding(16.dp)) {
        Text(name, style = MaterialTheme.typography.headlineSmall)
        Button(onClick = onBack) { Text("назад") }

        LazyColumn(modifier = Modifier.padding(top = 8.dp).weight(1f)) {
            items(links) { link ->
                Text(link.label, style = MaterialTheme.typography.labelSmall)
                Text(link.url, style = MaterialTheme.typography.bodySmall)
            }

            if (message.isNotEmpty()) {
                item {
                    Text(message, style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(top = 8.dp))
                }
            }
        }

        Button(onClick = { act { renewUser(app, name) } }, enabled = !busy) { Text("продлить 30 дней") }
        Button(onClick = { act { disableUser(app, name) } }, enabled = !busy) { Text("выключить") }
        Button(
            onClick = {
                act {
                    val text = app?.share(name) ?: return@act "нет состояния"
                    message = text
                    copyToClipboard(ctx, text)
                    "сообщение в буфере"
                }
            },
            enabled = !busy,
        ) { Text("сообщение") }
        Button(onClick = { act { pushState(app) } }, enabled = !busy) { Text("отправить в git") }

        if (status.isNotEmpty()) {
            Text(status, style = MaterialTheme.typography.bodySmall)
        }
    }
}

/**
 * onIo runs a call that talks to git or a host. Compose buttons run on the UI
 * thread, and Android kills the process on a network call there. Suspending is
 * the point: runBlocking would keep the frame blocked for the whole call, which
 * is what put "Application Not Responding" on top of a twenty second apply.
 */
suspend fun onIo(block: suspend () -> String): String = withContext(Dispatchers.IO) {
    try {
        block()
    } catch (e: Exception) {
        "ошибка: ${e.message}"
    }
}

fun renewUser(app: mobile.App?, name: String): String {
    val msg = app?.renew(name, 30L) ?: return "нет состояния"
    return "ok: $msg"
}

fun disableUser(app: mobile.App?, name: String): String {
    val msg = app?.setEnabled(name, false) ?: return "нет состояния"
    return "ok: $msg"
}

fun applyAll(app: mobile.App?): String {
    // an empty host means every host, leaves first: the same order apply uses
    val result = app?.apply("") ?: return "нет состояния"
    return if (result.contains("\"failed\"")) "ошибка: $result" else "применено"
}

/** addUser joins the sets the way the shell does: comma separated lists. */
fun addUser(app: mobile.App?, name: String, modes: Set<String>, protocols: Set<String>, tg: Boolean): String {
    val msg = app?.addUser(name, modes.joinToString(","), protocols.joinToString(","), tg) ?: return "нет состояния"
    return "ok: $msg"
}

/** copyToClipboard is how the message reaches the user: the phone has no mail. */
fun copyToClipboard(ctx: android.content.Context, text: String) {
    val clip = ctx.getSystemService(android.content.Context.CLIPBOARD_SERVICE) as android.content.ClipboardManager
    clip.setPrimaryClip(android.content.ClipData.newPlainText("vpnctl", text))
}

fun pushState(app: mobile.App?): String {
    val head = app?.push() ?: return "нет репозитория"
    return "pushed $head"
}

/** SelectRow is the one shape every choice on the create screen has: a set the
 * tap replaces (mode) or extends (protocols). */
@Composable
fun SelectRow(label: String, options: List<String>, selected: Set<String>, onPick: (String) -> Unit) {
    Text(label, style = MaterialTheme.typography.labelMedium)

    Row {
        options.forEach { option ->
            Button(onClick = { onPick(option) }, modifier = Modifier.padding(end = 8.dp)) {
                Text(if (option in selected) "✓ $option" else option)
            }
        }
    }
}

/**
 * AddUserScreen creates a user with fresh secrets. The core writes, validates
 * and commits; the phone only chooses the name, the mode and the protocols. The
 * hosts get the user on the next apply, not now.
 */
@Composable
fun AddUserScreen(ctx: android.content.Context, modifier: Modifier, onBack: () -> Unit, onCreated: () -> Unit) {
    val app = remember { Core.current() }
    var name by remember { mutableStateOf("") }
    var mode by remember { mutableStateOf(setOf("split")) }
    var protos by remember { mutableStateOf(setOf("reality", "ws")) }
    var tg by remember { mutableStateOf(false) }
    var status by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }

    val scope = rememberCoroutineScope()

    fun create() {
        if (busy || name.isBlank() || protos.isEmpty()) {
            return
        }

        busy = true
        status = "…"

        scope.launch {
            val out = onIo { addUser(app, name.trim(), mode, protos, tg) }

            busy = false
            status = out

            if (out.startsWith("ok")) {
                onCreated()
            }
        }
    }

    Column(modifier = modifier.fillMaxSize().padding(16.dp)) {
        Text("новый пользователь", style = MaterialTheme.typography.headlineSmall)
        Button(onClick = onBack) { Text("назад") }
        OutlinedTextField(value = name, onValueChange = { name = it }, label = { Text("имя") }, singleLine = true)
        SelectRow("режим", listOf("split", "full"), mode) { mode = setOf(it) }
        SelectRow("протоколы", listOf("reality", "ws", "hy2", "awg"), protos) {
            protos = if (it in protos) protos - it else protos + it
        }

        Row {
            Checkbox(checked = tg, onCheckedChange = { tg = it })
            Text("телеграм", modifier = Modifier.padding(top = 12.dp))
        }

        Button(onClick = { create() }, enabled = !busy && name.isNotBlank() && protos.isNotEmpty()) { Text("создать") }
        Text(status, style = MaterialTheme.typography.bodySmall)
    }
}

/** PlanScreen shows what apply would change on every host. */
@Composable
fun PlanScreen(ctx: android.content.Context, modifier: Modifier, onBack: () -> Unit) {
    val app = remember { Core.current() }
    var status by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var hosts by remember { mutableStateOf(emptyList<PlanHost>()) }

    val scope = rememberCoroutineScope()

    suspend fun load() {
        busy = true
        status = "…"
        status = onIo {
            val json = app?.plan("") ?: return@onIo "нет состояния"
            hosts = parsePlan(json)
            "hosts: ${hosts.size}"
        }
        busy = false
    }

    // One action at a time, for the same reason as the user card: the agent and
    // the hosts see a series of requests and a second apply would interleave.
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

    LaunchedEffect(Unit) { load() }

    Column(modifier = modifier.fillMaxSize().padding(16.dp)) {
        Text("план", style = MaterialTheme.typography.headlineSmall)
        Button(onClick = onBack) { Text("назад") }
        Button(
            onClick = {
                act {
                    val out = applyAll(app)
                    // the hosts just changed: show what is left, not what was
                    if (out == "применено") {
                        load()
                    }

                    out
                }
            },
            enabled = !busy,
        ) { Text("применить") }
        Text(status, style = MaterialTheme.typography.bodySmall)

        LazyColumn {
            items(hosts) { h ->
                Text("${h.name}: ${h.changes} изменений", modifier = Modifier.padding(vertical = 4.dp))
                if (h.unreachable.isNotEmpty()) {
                    Text(h.unreachable, style = MaterialTheme.typography.bodySmall)
                }
            }
        }
    }
}

const val PLAN = "план"
const val ADD = "новый"
const val REPO = "git@github.com:danilrwx/vpn.git"
