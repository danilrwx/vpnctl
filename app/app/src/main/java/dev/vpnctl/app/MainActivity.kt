package dev.vpnctl.app

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Checkbox
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
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
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import com.google.zxing.BarcodeFormat
import com.google.zxing.EncodeHintType
import com.google.zxing.common.BitMatrix
import com.google.zxing.qrcode.QRCodeWriter
import com.google.zxing.qrcode.decoder.ErrorCorrectionLevel
import kotlin.math.ceil
import kotlin.math.floor
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import mobile.Mobile
import org.json.JSONArray
import java.io.File

/** One row of the list screen. */
data class UserRow(
    val name: String,
    val modes: String,
    val protocols: List<String>,
    val tg: Boolean,
    val enabled: Boolean,
    val expires: String,
)

/** One copyable line of a user card. */
data class UserLink(val label: String, val url: String)

/** One recipient of secrets.age.yaml. */
data class DeviceRow(val name: String, val recipient: String, val added: String)

/** parseDevices reads who can read the secrets. */
fun parseDevices(json: String): List<DeviceRow> = try {
    val arr = JSONArray(json)
    (0 until arr.length()).map { i ->
        val o = arr.getJSONObject(i)
        DeviceRow(o.getString("name"), o.getString("recipient"), o.optString("added"))
    }
} catch (e: Exception) {
    emptyList()
}

/** One link between two hosts, as the state describes it. */
data class LinkRow(val from: String, val to: String, val active: String, val protocols: String)

/** parseHostLinks reads the node-to-node links. */
fun parseHostLinks(json: String): List<LinkRow> = try {
    val arr = JSONArray(json)
    (0 until arr.length()).map { i ->
        val o = arr.getJSONObject(i)
        val protos = o.optJSONArray("protocols")

        LinkRow(
            from = o.getString("from"),
            to = o.getString("to"),
            active = o.optString("active"),
            protocols = if (protos == null) "" else (0 until protos.length()).joinToString(" ") { protos.getString(it) },
        )
    }
} catch (e: Exception) {
    emptyList()
}

/** One host of the estate: what the state says, before anything answers. */
data class HostRow(
    val name: String,
    val role: String,
    val engine: String,
    val domain: String,
    val ip: String,
    val via: String,
    val retiring: Boolean,
)

/** parseHosts reads the estate the core returns. */
fun parseHosts(json: String): List<HostRow> = try {
    val arr = JSONArray(json)
    (0 until arr.length()).map { i ->
        val o = arr.getJSONObject(i)
        HostRow(
            name = o.getString("name"),
            role = o.getString("role"),
            engine = o.getString("engine"),
            domain = o.optString("domain"),
            ip = o.optString("ip"),
            via = o.optString("via"),
            retiring = o.optBoolean("retiring"),
        )
    }
} catch (e: Exception) {
    emptyList()
}

/** One file of the plan, with the mark the core gives it: ~ changed, + new, - removed. */
data class PlanFile(val path: String, val status: String)

/** One host of the plan: what apply would do, and why it might not. */
data class PlanHost(
    val name: String,
    val changes: Int,
    val unreachable: String,
    val marker: String,
    val bootstrapped: Boolean,
    val notes: List<String>,
    val files: List<PlanFile>,
)

/** parsePlan reads what plan.JSONOf writes: a host, a change count, files. */
fun parsePlan(json: String): List<PlanHost> = try {
    val arr = JSONArray(json)
    (0 until arr.length()).map { i ->
        val o = arr.getJSONObject(i)
        val notes = o.optJSONArray("notes")
        val files = o.optJSONArray("files")

        PlanHost(
            name = o.getString("name"),
            changes = o.getInt("changes"),
            unreachable = o.optString("unreachable"),
            marker = o.optString("marker"),
            bootstrapped = o.optBoolean("bootstrapped"),
            notes = if (notes == null) emptyList() else (0 until notes.length()).map { notes.getString(it) },
            files = if (files == null) {
                emptyList()
            } else {
                (0 until files.length()).map {
                    val f = files.getJSONObject(it)
                    PlanFile(f.getString("path"), f.getString("status"))
                }
            },
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
        val protos = o.optJSONArray("protocols")
        UserRow(
            name = o.getString("name"),
            modes = (0 until modes.length()).joinToString("/") { modes.getString(it) },
            protocols = if (protos == null) emptyList() else (0 until protos.length()).map { protos.getString(it) },
            tg = o.optBoolean("tg"),
            enabled = o.getBoolean("enabled"),
            expires = o.optString("expires"),
        )
    }
} catch (e: Exception) {
    emptyList()
}

/** parseProtocols reads the entry's own inbound names. */
fun parseProtocols(json: String): List<String> = try {
    val arr = JSONArray(json)
    (0 until arr.length()).map { arr.getString(it) }
} catch (e: Exception) {
    emptyList()
}

/**
 * userProtocols keeps what a user can be given on this entry, in a fixed order.
 * The entry carries mtg too, but that is the Telegram flag, not a client
 * protocol, and offering one the entry has no inbound for fails at save.
 */
fun userProtocols(served: List<String>): List<String> {
    val out = listOf("reality", "ws", "hy2", "awg").filter { it in served }

    return if (out.isEmpty()) listOf("reality", "ws") else out
}

/** servedProtocols is the entry's own answer, cut to what a user can have. */
fun servedProtocols(app: mobile.App?): List<String> =
    userProtocols(app?.let { parseProtocols(it.protocols()) } ?: emptyList())

/** defaultProtocols is what the command line creates when it is not told: the
 * pair the entry serves, or its first protocol when it serves only one. */
fun defaultProtocols(served: List<String>): Set<String> {
    val both = listOf("reality", "ws").filter { it in served }

    return if (both.isEmpty()) setOf(served.first()) else both.toSet()
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
        HOSTS -> HostsScreen(ctx, modifier, onBack = { selected = null })
        OVERVIEW -> OverviewScreen(ctx, modifier, onBack = { selected = null })
        DEVICES -> DevicesScreen(ctx, modifier, onBack = { selected = null })
        ADD -> AddUserScreen(
            ctx,
            modifier,
            onBack = { selected = null },
            onCreated = {
                version++
                selected = null
            },
        )
        else -> UserScreen(
            ctx,
            modifier,
            name = selected!!,
            onBack = { selected = null },
            onDeleted = {
                version++
                selected = null
            },
        )
    }
}

@Composable
fun UsersScreen(ctx: android.content.Context, modifier: Modifier, version: Int, onPick: (String) -> Unit) {
    var status by remember { mutableStateOf("") }
    // The handle is read in composition, not kept in state: a Go object parked in
    // Compose state did not survive the open (the write never reached the read),
    // and the list stayed on the APK fixture until the process restarted. Reading
    // it needs a reason to recompose, and a successful open has nothing to say,
    // so the counter is the reason.
    var epoch by remember { mutableStateOf(0) }

    LaunchedEffect(Unit) {
        if (Core.current() == null) {
            val opened = withContext(Dispatchers.IO) { Core.openBundled(ctx) }
            if (opened == null) {
                status = "no state"
            }

            epoch++
        }

        val repo = Prefs.repo(ctx)
        if (Core.current() != null && !File(ctx.filesDir, "state/.git").exists()) {
            if (repo.isEmpty()) {
                status = "not connected · open Devices to point the app at a repository"
            } else {
                status = withContext(Dispatchers.IO) { Core.sync(ctx, repo) }
                epoch++
            }
        }
    }

    val app = Core.current()
    val users = remember(app, epoch, version) { app?.let { parseUsers(it.users()) } ?: emptyList() }

    Column(modifier = modifier.fillMaxSize().padding(16.dp)) {
        if (status.isNotEmpty()) {
            Text(status, style = MaterialTheme.typography.bodySmall)
        }

        Text("Users ${users.size} · ${users.count { it.enabled }} active", style = MaterialTheme.typography.titleMedium)
        Row {
            Button(onClick = { onPick(OVERVIEW) }) { Text("Overview") }
            Button(onClick = { onPick(PLAN) }, modifier = Modifier.padding(start = 8.dp)) { Text("Plan") }
        }

        Row {
            Button(onClick = { onPick(HOSTS) }) { Text("Hosts") }
            Button(onClick = { onPick(ADD) }, modifier = Modifier.padding(start = 8.dp)) { Text("New") }
            Button(onClick = { onPick(DEVICES) }, modifier = Modifier.padding(start = 8.dp)) { Text("Devices") }
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
fun UserScreen(
    ctx: android.content.Context,
    modifier: Modifier,
    name: String,
    onBack: () -> Unit,
    onDeleted: () -> Unit,
) {
    val app = remember { Core.current() }
    val served = remember { servedProtocols(app) }
    var status by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var links by remember { mutableStateOf(emptyList<UserLink>()) }
    var message by remember { mutableStateOf("") }
    var qr by remember { mutableStateOf("") }
    var mode by remember { mutableStateOf(setOf("split")) }
    var protos by remember { mutableStateOf(emptySet<String>()) }
    var tg by remember { mutableStateOf(false) }
    var expires by remember { mutableStateOf("") }
    var enabled by remember { mutableStateOf(true) }
    var lang by remember { mutableStateOf("ru") }
    var deleting by remember { mutableStateOf(false) }
    var typed by remember { mutableStateOf("") }
    var reload by remember { mutableStateOf(0) }

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

    LaunchedEffect(name, reload) {
        links = withContext(Dispatchers.IO) {
            app?.let { parseLinks(it.links(name)) } ?: emptyList()
        }

        val row = withContext(Dispatchers.IO) {
            app?.let { parseUsers(it.users()).firstOrNull { u -> u.name == name } }
        }

        if (row != null) {
            mode = row.modes.split("/").toSet()
            protos = row.protocols.toSet()
            tg = row.tg
            expires = row.expires
            enabled = row.enabled
        }
    }

    Column(modifier = modifier.fillMaxSize().padding(16.dp)) {
        Text(name, style = MaterialTheme.typography.headlineSmall)
        Button(onClick = onBack) { Text("Back") }

        // The card holds the links, the form and the actions. It all scrolls as
        // one list: a fixed column pushed the last buttons off the screen.
        LazyColumn(modifier = Modifier.weight(1f)) {
            items(links) { link ->
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Column(modifier = Modifier.weight(1f)) {
                        Text(link.label, style = MaterialTheme.typography.labelSmall)
                        Text(link.url, style = MaterialTheme.typography.bodySmall)
                    }

                    TextButton(onClick = {
                        copyToClipboard(ctx, link.url)
                        status = "${link.label}: copied"
                    }) { Text("Copy") }

                    TextButton(onClick = { qr = link.url }) { Text("QR") }
                }
            }

            if (message.isNotEmpty()) {
                item {
                    Text(message, style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(top = 8.dp))
                }
            }

            item {
                SelectRow("Modes", listOf("split", "full"), mode) { mode = setOf(it) }
            }

            item {
                SelectRow("Protocols", served, protos) {
                    protos = if (it in protos) protos - it else protos + it
                }
            }

            item {
                Row {
                    Checkbox(checked = tg, onCheckedChange = { tg = it })
                    Text("Telegram proxy", modifier = Modifier.padding(top = 12.dp))
                }
            }

            item {
                OutlinedTextField(
                    value = expires,
                    onValueChange = { expires = it },
                    label = { Text("Expiry YYYY-MM-DD, empty — none") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
            }

            item {
                Button(
                    onClick = {
                        act {
                            val out = saveUser(app, name, mode, protos, tg, expires)
                            if (out.startsWith("ok")) {
                                reload++
                            }

                            out
                        }
                    },
                    enabled = !busy && protos.isNotEmpty(),
                ) { Text("Save") }
            }

            item {
                Button(onClick = { act { renewUser(app, name) } }, enabled = !busy) { Text("Renew 30 days") }
            }

            item {
                // the same button both ways: an account turned off by mistake has
                // to come back from the phone, not from a terminal
                Button(
                    onClick = {
                        act {
                            val out = setUserEnabled(app, name, !enabled)
                            if (out.startsWith("ok")) {
                                reload++
                            }

                            out
                        }
                    },
                    enabled = !busy,
                ) { Text(if (enabled) "Disable" else "Enable") }
            }

            item {
                SelectRow("Message language", listOf("ru", "en"), setOf(lang)) { lang = it }
            }

            item {
                Button(
                    onClick = {
                        act {
                            val text = app?.message(name, lang, false) ?: return@act "no state"
                            message = text
                            copyToClipboard(ctx, text)
                            "message copied"
                        }
                    },
                    enabled = !busy,
                ) { Text("Message") }
            }

            item {
                Button(onClick = { act { pushState(app) } }, enabled = !busy) { Text("Push") }
            }

            item {
                Button(
                    onClick = {
                        typed = ""
                        deleting = true
                    },
                    enabled = !busy,
                ) { Text("Delete") }
            }

        }

        // The answer stays pinned: the action that produced it can be anywhere in
        // the list, and an error the user has to scroll for reads as nothing.
        if (status.isNotEmpty()) {
            Text(status, style = MaterialTheme.typography.bodySmall)
        }
    }

    if (qr.isNotEmpty()) {
        QrDialog(links.firstOrNull { it.url == qr }?.label ?: "QR", qr, onDismiss = { qr = "" })
    }

    if (deleting) {
        DeleteDialog(
            name = name,
            accounts = mode.size * protos.size,
            subs = mode.size,
            enabled = enabled,
            busy = busy,
            typed = typed,
            onTyped = { typed = it },
            onDismiss = { deleting = false },
            onDisable = {
                deleting = false

                act {
                    val out = setUserEnabled(app, name, false)
                    if (out.startsWith("ok")) {
                        reload++
                    }

                    out
                }
            },
            onDelete = {
                deleting = false

                act {
                    val out = removeUser(app, name)
                    if (out.startsWith("ok")) {
                        onDeleted()
                    }

                    out
                }
            },
        )
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
        "error: ${e.message}"
    }
}

fun renewUser(app: mobile.App?, name: String): String {
    val msg = app?.renew(name, 30L) ?: return "no state"
    return "ok: $msg"
}

fun setUserEnabled(app: mobile.App?, name: String, on: Boolean): String {
    val msg = app?.setEnabled(name, on) ?: return "no state"
    return "ok: $msg"
}

/** addUser joins the sets the way the shell does: comma separated lists. */
fun addUser(app: mobile.App?, name: String, modes: Set<String>, protocols: Set<String>, tg: Boolean): String {
    val msg = app?.addUser(name, modes.joinToString(","), protocols.joinToString(","), tg) ?: return "no state"
    return "ok: $msg"
}

/** copyToClipboard is how the message reaches the user: the phone has no mail. */
fun copyToClipboard(ctx: android.content.Context, text: String) {
    val clip = ctx.getSystemService(android.content.Context.CLIPBOARD_SERVICE) as android.content.ClipboardManager
    clip.setPrimaryClip(android.content.ClipData.newPlainText("vpnctl", text))
}

/** saveUser sends the whole form back: the screen shows the state it edits. */
fun saveUser(
    app: mobile.App?,
    name: String,
    modes: Set<String>,
    protocols: Set<String>,
    tg: Boolean,
    expires: String,
): String {
    val msg = app?.editUser(name, modes.joinToString(","), protocols.joinToString(","), tg, expires.trim())
        ?: return "no state"

    return "ok: $msg"
}

fun removeUser(app: mobile.App?, name: String): String {
    val msg = app?.rmUser(name) ?: return "no state"
    return "ok: $msg"
}

fun pushState(app: mobile.App?): String {
    val head = app?.push() ?: return "no repository"
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
    val served = remember { servedProtocols(app) }
    var name by remember { mutableStateOf("") }
    var mode by remember { mutableStateOf(setOf("split")) }
    var protos by remember { mutableStateOf(defaultProtocols(served)) }
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
        Text("New user", style = MaterialTheme.typography.headlineSmall)
        Button(onClick = onBack) { Text("Back") }
        OutlinedTextField(value = name, onValueChange = { name = it }, label = { Text("Name") }, singleLine = true)
        SelectRow("Modes", listOf("split", "full"), mode) { mode = setOf(it) }
        SelectRow("Protocols", served, protos) {
            protos = if (it in protos) protos - it else protos + it
        }

        Row {
            Checkbox(checked = tg, onCheckedChange = { tg = it })
            Text("Telegram proxy", modifier = Modifier.padding(top = 12.dp))
        }

        Button(onClick = { create() }, enabled = !busy && name.isNotBlank() && protos.isNotEmpty()) { Text("Create") }
        Text(status, style = MaterialTheme.typography.bodySmall)
    }
}

/** QrDialog shows a link as a QR: the phone is where a link is handed over. */
@Composable
fun QrDialog(label: String, url: String, onDismiss: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(label) },
        text = {
            Column(horizontalAlignment = Alignment.CenterHorizontally) {
                QrCode(url, 220.dp)
                Text(url, style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(top = 12.dp))
            }
        },
        confirmButton = { TextButton(onClick = onDismiss) { Text("Close") } },
    )
}

/** QrCode draws the matrix the encoder returns, one square per module. */
@Composable
fun QrCode(text: String, size: Dp) {
    val matrix = remember(text) { qrMatrix(text) }

    Canvas(modifier = Modifier.size(size)) {
        val n = matrix.width
        val step = this.size.minDimension / n

        drawRect(Color.White)

        for (x in 0 until n) {
            for (y in 0 until n) {
                if (!matrix.get(x, y)) {
                    continue
                }
                // snapped edges that overlap by a fraction of a pixel: a shared
                // edge drawn twice is invisible, a gap between modules is not
                val left = floor(x * step)
                val top = floor(y * step)

                drawRect(Color.Black, Offset(left, top), Size(ceil((x + 1) * step) - left, ceil((y + 1) * step) - top))
            }
        }
    }
}

/** qrMatrix asks for one pixel per module (0 means "the natural size"): asking
 * for a big matrix makes the encoder scale it up, and then the canvas draws a
 * rectangle per *pixel* — 660² of them, which is an ANR, not a QR code. */
fun qrMatrix(text: String): BitMatrix = QRCodeWriter().encode(
    text,
    BarcodeFormat.QR_CODE,
    0,
    0,
    mapOf<EncodeHintType, Any>(EncodeHintType.MARGIN to 1, EncodeHintType.ERROR_CORRECTION to ErrorCorrectionLevel.M),
)

/**
 * DevicesScreen is both halves of onboarding: where the state comes from, and
 * who can read it. The repository field replaces a constant that was compiled
 * in, and the keys below are what have to be filed on the forge and the hosts
 * before either works.
 */
@Composable
fun DevicesScreen(ctx: android.content.Context, modifier: Modifier, onBack: () -> Unit) {
    var devices by remember { mutableStateOf(emptyList<DeviceRow>()) }
    var repo by remember { mutableStateOf(Prefs.repo(ctx).ifEmpty { REPO }) }
    var status by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    // the core is reopened after a restore, and the recipient file changes with
    // it: this counter is what makes the screen read both again
    var version by remember { mutableStateOf(0) }
    var exporting by remember { mutableStateOf(false) }
    var restoring by remember { mutableStateOf(false) }
    // a failure belongs next to the fields that caused it: the status line of
    // the screen sits behind the dialog, and an error nobody sees reads as a
    // button that does nothing
    var exportErr by remember { mutableStateOf("") }
    var restoreErr by remember { mutableStateOf("") }
    var revoking by remember { mutableStateOf<DeviceRow?>(null) }

    val scope = rememberCoroutineScope()
    val recipient = remember(version) { localRecipient(ctx) }
    val sshKey = remember { File(ctx.filesDir, "identity.pub").takeIf { it.exists() }?.readText()?.trim().orEmpty() }

    LaunchedEffect(version) {
        devices = withContext(Dispatchers.IO) {
            Core.current()?.let { parseDevices(it.devices()) } ?: emptyList()
        }
    }

    fun connect() {
        if (busy || repo.isBlank()) {
            return
        }

        busy = true
        status = "…"

        scope.launch {
            Prefs.setRepo(ctx, repo)
            status = onIo { Core.connect(ctx, repo) }
            version++
            busy = false
        }
    }

    Column(modifier = modifier.fillMaxSize().padding(16.dp)) {
        Text("Devices and keys", style = MaterialTheme.typography.headlineSmall)
        Text(
            "who can read the secrets, and what happens when a device is gone",
            style = MaterialTheme.typography.bodySmall,
        )
        Button(onClick = onBack) { Text("Back") }

        LazyColumn(modifier = Modifier.weight(1f)) {
            item {
                Text("Repository", style = MaterialTheme.typography.titleSmall, modifier = Modifier.padding(top = 12.dp))
                Text(
                    "the phone clones with its own deploy key; connecting replaces the local copy",
                    style = MaterialTheme.typography.bodySmall,
                )
            }

            item {
                OutlinedTextField(
                    value = repo,
                    onValueChange = { repo = it },
                    label = { Text("git@github.com:owner/state.git") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
            }

            item {
                Row {
                    Button(onClick = { connect() }, enabled = !busy && repo.isNotBlank()) { Text("Connect") }
                    TextButton(
                        onClick = {
                            busy = true
                            scope.launch {
                                status = onIo { pushState(Core.current()) }
                                busy = false
                            }
                        },
                        enabled = !busy,
                    ) { Text("Push") }
                }
            }

            item {
                Text("This device", style = MaterialTheme.typography.titleSmall, modifier = Modifier.padding(top = 16.dp))
                Text("age recipient", style = MaterialTheme.typography.bodySmall)
                Text(recipient.ifEmpty { "none yet" }, style = MaterialTheme.typography.bodySmall)
                TextButton(onClick = { copyToClipboard(ctx, recipient) }, enabled = recipient.isNotEmpty()) { Text("Copy recipient") }
                Text("ssh key for the agent", style = MaterialTheme.typography.bodySmall)
                Text(sshKey.ifEmpty { "none yet" }, style = MaterialTheme.typography.bodySmall)
                TextButton(onClick = { copyToClipboard(ctx, sshKey) }, enabled = sshKey.isNotEmpty()) { Text("Copy key") }
            }

            item {
                Text("Identity backup", style = MaterialTheme.typography.titleSmall, modifier = Modifier.padding(top = 16.dp))
                Text(
                    "the age key lives in the Keystore, which dies with the app: a passphrase copy is the only way back",
                    style = MaterialTheme.typography.bodySmall,
                )
                Row {
                    TextButton(onClick = { exportErr = ""; exporting = true }, enabled = !busy) { Text("Export") }
                    TextButton(onClick = { restoreErr = ""; restoring = true }, enabled = !busy) { Text("Restore or transfer") }
                }
            }

            item {
                Text(
                    "Recipients · ${devices.size}",
                    style = MaterialTheme.typography.titleSmall,
                    modifier = Modifier.padding(top = 16.dp),
                )
            }

            items(devices) { d ->
                Column(modifier = Modifier.padding(vertical = 6.dp)) {
                    Row {
                        Text(d.name, style = MaterialTheme.typography.titleSmall)

                        if (d.recipient == recipient) {
                            Text("  this phone", style = MaterialTheme.typography.bodySmall)
                        }
                    }

                    Text(d.recipient, style = MaterialTheme.typography.bodySmall)
                    if (d.added.isNotEmpty()) {
                        Text("added ${d.added}", style = MaterialTheme.typography.bodySmall)
                    }

                    if (d.recipient != recipient) {
                        TextButton(onClick = { revoking = d }, enabled = !busy) { Text("Revoke") }
                    }
                }
            }

            item {
                Text(
                    "every recipient can read secrets.age.yaml; private keys never leave their device",
                    style = MaterialTheme.typography.bodySmall,
                    modifier = Modifier.padding(top = 12.dp),
                )
            }
        }

        if (status.isNotEmpty()) {
            Text(status, style = MaterialTheme.typography.bodySmall)
        }
    }

    if (exporting) {
        ExportIdentityDialog(
            busy = busy,
            error = exportErr,
            onDismiss = { exporting = false },
            onSeal = { pass ->
                busy = true
                scope.launch {
                    val (blob, err) = withContext(Dispatchers.IO) { sealIdentityText(pass) }
                    if (err.isEmpty()) {
                        status = "sealed and copied · store it off this phone"
                        copyToClipboard(ctx, blob)
                        exporting = false
                    } else {
                        exportErr = err
                    }
                    busy = false
                }
            },
        )
    }

    if (restoring) {
        RestoreIdentityDialog(
            busy = busy,
            error = restoreErr,
            onDismiss = { restoring = false },
            onRestore = { blob, pass ->
                busy = true
                scope.launch {
                    val (msg, err) = withContext(Dispatchers.IO) { restoreIdentityText(ctx, blob, pass) }
                    if (err.isEmpty()) {
                        status = msg
                        version++
                        restoring = false
                    } else {
                        restoreErr = err
                    }

                    busy = false
                }
            },
        )
    }

    revoking?.let { d ->
        RevokeDialog(
            name = d.name,
            busy = busy,
            onDismiss = { revoking = null },
            onRevoke = {
                busy = true
                scope.launch {
                    status = onIo { revokeDevice(Core.current(), d.name) }
                    version++
                    revoking = null
                    busy = false
                }
            },
        )
    }
}

/**
 * ExportIdentityDialog seals the phone's own identity. The blob and the password
 * are together a copy of the key: the dialog says so, because it is the one
 * secret the app can reproduce in the clear.
 */
@Composable
fun ExportIdentityDialog(busy: Boolean, error: String, onDismiss: () -> Unit, onSeal: (String) -> Unit) {
    var pass by remember { mutableStateOf("") }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Export this identity") },
        text = {
            Column {
                Text(
                    "The text is the age key under this password. Anyone holding both reads every secret, and nothing ties it to this phone.",
                )
                Text(
                    "At least 8 characters. Store it away from the repository.",
                    style = MaterialTheme.typography.bodySmall,
                    modifier = Modifier.padding(top = 8.dp),
                )
                OutlinedTextField(
                    value = pass,
                    onValueChange = { pass = it },
                    label = { Text("Password") },
                    singleLine = true,
                    visualTransformation = PasswordVisualTransformation(),
                    modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
                )
                if (busy) {
                    Text("sealing…", style = MaterialTheme.typography.bodySmall)
                }

                if (error.isNotEmpty()) {
                    Text(error, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
                }
            }
        },
        confirmButton = {
            Button(onClick = { onSeal(pass) }, enabled = !busy && pass.length >= 8) { Text("Seal and copy") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

/**
 * RestoreIdentityDialog takes a blob and its password. The core unseals it and,
 * when the state is here, proves it decrypts secrets.age.yaml before the phone
 * replaces the key it is using.
 */
@Composable
fun RestoreIdentityDialog(busy: Boolean, error: String, onDismiss: () -> Unit, onRestore: (String, String) -> Unit) {
    var blob by remember { mutableStateOf("") }
    var pass by remember { mutableStateOf("") }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Restore with a password") },
        text = {
            Column {
                Text("Paste the backup you stored at setup. The identity replaces this phone's own; a key that does not decrypt the secrets here is refused.")
                OutlinedTextField(
                    value = blob,
                    onValueChange = { blob = it },
                    label = { Text("-----BEGIN AGE ENCRYPTED FILE-----") },
                    minLines = 3,
                    maxLines = 6,
                    modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
                )
                OutlinedTextField(
                    value = pass,
                    onValueChange = { pass = it },
                    label = { Text("Password") },
                    singleLine = true,
                    visualTransformation = PasswordVisualTransformation(),
                    modifier = Modifier.fillMaxWidth().padding(top = 8.dp),
                )
                if (busy) {
                    Text("unlocking…", style = MaterialTheme.typography.bodySmall)
                }

                if (error.isNotEmpty()) {
                    Text(error, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
                }
            }
        },
        confirmButton = {
            Button(onClick = { onRestore(blob, pass) }, enabled = !busy && blob.isNotBlank() && pass.isNotEmpty()) {
                Text("Restore")
            }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

/**
 * RevokeDialog asks for the name, as the delete dialog does, and spells out what
 * revocation cannot do: old commits keep the secrets that device could read.
 */
@Composable
fun RevokeDialog(name: String, busy: Boolean, onDismiss: () -> Unit, onRevoke: () -> Unit) {
    var typed by remember { mutableStateOf("") }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Revoke $name?") },
        text = {
            Column {
                Text("· age.yaml loses this recipient and secrets.age.yaml is re-encrypted for the rest")
                Text("· this phone keeps its own access")
                Text("· old commits stay readable with this key: assume it has read them")
                Text("· its ssh key stays in authorized_keys on the hosts until it is removed there by hand")
                Text(
                    "Type the name to confirm",
                    style = MaterialTheme.typography.labelMedium,
                    modifier = Modifier.padding(top = 12.dp, bottom = 6.dp),
                )
                OutlinedTextField(
                    value = typed,
                    onValueChange = { typed = it },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        },
        confirmButton = {
            Button(onClick = onRevoke, enabled = !busy && typed.trim() == name) { Text("Revoke") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

/** sealIdentityText returns the armored blob and an error line, never both. */
fun sealIdentityText(pass: String): Pair<String, String> = try {
    Mobile.sealIdentity(pass) to ""
} catch (e: Exception) {
    "" to "error: ${e.message}"
}

/**
 * restoreIdentityText swaps the phone's key for the one in the blob. The Vault
 * write happens only after the core has proven the key decrypts the state, so a
 * wrong backup cannot leave the app with a key that opens nothing.
 */
fun restoreIdentityText(ctx: android.content.Context, blob: String, pass: String): Pair<String, String> {
    val dir = File(ctx.filesDir, "state").absolutePath

    return try {
        val id = Mobile.unsealIdentity(blob, pass, dir)
        Vault(ctx.filesDir).put("age.key", id.secret)
        File(ctx.filesDir, "age.recipient").writeText(id.recipient)

        // unseal refuses a key that cannot decrypt the local secrets, so getting
        // here means the check passed; say which check, because the state may
        // also not be here yet
        val checked = if (File(dir, "secrets.age.yaml").exists()) " · decrypts secrets.age.yaml" else " · no secrets here to check yet"
        val opened = Core.reopen(ctx)

        if (opened == null) {
            "restored ${id.recipient}$checked, but the state did not open" to ""
        } else {
            "restored ${id.recipient}$checked" to ""
        }
    } catch (e: Exception) {
        "" to "error: ${restoreFailure(e.message.orEmpty())}"
    }
}

/**
 * restoreFailure keeps the age library's error chain out of the dialog: it names
 * the same thing three times ("incorrect passphrase" twice), and what the user
 * has to act on is whether the password or the backup is wrong.
 */
fun restoreFailure(message: String): String = when {
    message.contains("wrong passphrase") -> "wrong password, or the backup is damaged"
    message.contains("does not decrypt") -> "this key does not read the secrets here: it is not a recipient of age.yaml"
    else -> message.substringAfter("error: ").trim()
}

/** revokeDevice drops a recipient and re-encrypts the secrets for the rest. */
fun revokeDevice(app: mobile.App?, name: String): String {
    if (app == null) {
        return "no state"
    }

    return try {
        "ok: ${app.removeDevice(name)} · push when the forge is reachable"
    } catch (e: Exception) {
        "error: ${e.message}"
    }
}

/** localRecipient is the age recipient this phone minted on first launch. */
fun localRecipient(ctx: android.content.Context): String =
    File(ctx.filesDir, "age.recipient").takeIf { it.exists() }?.readText()?.trim().orEmpty()

/**
 * OverviewScreen is the state at a glance: users, hosts, links and whether every
 * host runs the same commit. No traffic and no latency — the core measures
 * neither, and an empty slot is better than an invented number.
 */
@Composable
fun OverviewScreen(ctx: android.content.Context, modifier: Modifier, onBack: () -> Unit) {
    val app = remember { Core.current() }
    var users by remember { mutableStateOf(emptyList<UserRow>()) }
    var hosts by remember { mutableStateOf(emptyList<HostRow>()) }
    var links by remember { mutableStateOf(emptyList<LinkRow>()) }
    var plans by remember { mutableStateOf(mapOf<String, PlanHost>()) }
    var status by remember { mutableStateOf("") }

    LaunchedEffect(Unit) {
        users = withContext(Dispatchers.IO) { app?.let { parseUsers(it.users()) } ?: emptyList() }
        hosts = withContext(Dispatchers.IO) { app?.let { parseHosts(it.hosts()) } ?: emptyList() }
        links = withContext(Dispatchers.IO) { app?.let { parseHostLinks(it.hostLinks()) } ?: emptyList() }

        status = withContext(Dispatchers.IO) {
            val json = app?.plan("") ?: return@withContext "no state"

            plans = parsePlan(json).associateBy { it.name }
            "probed ${plans.size}"
        }
    }

    val active = users.count { it.enabled }
    val expiring = users.count { it.expires.isNotEmpty() }
    val entries = hosts.count { it.role == "entry" }
    val unreachable = plans.values.count { it.unreachable.isNotEmpty() }
    val markers = plans.values.map { it.marker }.filter { it.isNotEmpty() }.distinct()

    Column(modifier = modifier.fillMaxSize().padding(16.dp)) {
        Text("Overview", style = MaterialTheme.typography.headlineSmall)
        Button(onClick = onBack) { Text("Back") }

        Text("$active of ${users.size} active · $expiring with an expiry", modifier = Modifier.padding(top = 12.dp))
        Text(
            "${hosts.size} hosts · $entries ${if (entries == 1) "entry" else "entries"} · " +
                "${hosts.size - entries} ${if (hosts.size - entries == 1) "exit" else "exits"} · $unreachable unreachable",
            modifier = Modifier.padding(top = 6.dp),
        )
        Text(
            if (markers.isEmpty()) {
                "no markers read"
            } else {
                "markers: ${markers.joinToString(", ")} · ${plans.values.count { it.changes > 0 }} hosts with changes"
            },
            modifier = Modifier.padding(top = 6.dp),
        )

        LazyColumn(modifier = Modifier.padding(top = 12.dp).weight(1f)) {
            items(links) { l ->
                // active is one of the protocols when the link is healthy: say it once
                val what = if (l.active.isNotEmpty() && l.active != l.protocols) {
                    "${l.protocols} · ${l.active}"
                } else {
                    l.protocols
                }

                Text("${l.from} → ${l.to}  $what", modifier = Modifier.padding(vertical = 4.dp))
            }
        }

        if (status.isNotEmpty()) {
            Text(status, style = MaterialTheme.typography.bodySmall)
        }
    }
}

/**
 * HostsScreen is the estate as the state describes it, with the marker and
 * reachability a plan adds. It does not show latency or traffic: the core has
 * neither, and a dash is better than a number nobody measured.
 */
@Composable
fun HostsScreen(ctx: android.content.Context, modifier: Modifier, onBack: () -> Unit) {
    val app = remember { Core.current() }
    var status by remember { mutableStateOf("") }
    var rows by remember { mutableStateOf(emptyList<HostRow>()) }
    var plans by remember { mutableStateOf(mapOf<String, PlanHost>()) }

    LaunchedEffect(Unit) {
        rows = withContext(Dispatchers.IO) {
            app?.let { parseHosts(it.hosts()) } ?: emptyList()
        }

        status = withContext(Dispatchers.IO) {
            val json = app?.plan("") ?: return@withContext "no state"

            plans = parsePlan(json).associateBy { it.name }
            "${plans.size} probed"
        }
    }

    Column(modifier = modifier.fillMaxSize().padding(16.dp)) {
        Text("Hosts", style = MaterialTheme.typography.headlineSmall)
        Button(onClick = onBack) { Text("Back") }

        LazyColumn(modifier = Modifier.weight(1f)) {
            items(rows) { h ->
                val plan = plans[h.name]

                Column(modifier = Modifier.padding(vertical = 8.dp)) {
                    Row {
                        Text(h.name, style = MaterialTheme.typography.titleSmall)
                        Text("  ${h.role} · ${h.engine}", style = MaterialTheme.typography.bodySmall)
                    }

                    if (h.domain.isNotEmpty()) {
                        Text(h.domain, style = MaterialTheme.typography.bodySmall)
                    }

                    if (h.via.isNotEmpty()) {
                        Text("via ${h.via}", style = MaterialTheme.typography.bodySmall)
                    }

                    if (h.retiring) {
                        Text("retiring", style = MaterialTheme.typography.bodySmall)
                    }

                    when {
                        plan == null -> {}
                        plan.unreachable.isNotEmpty() -> Text(
                            plan.unreachable,
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.error,
                        )
                        !plan.bootstrapped -> Text("new · bootstrap not done", style = MaterialTheme.typography.bodySmall)
                        else -> Text(
                            "applied ${plan.marker.ifEmpty { "—" }} · ${plan.changes} changes",
                            style = MaterialTheme.typography.bodySmall,
                        )
                    }
                }
            }
        }

        if (status.isNotEmpty()) {
            Text(status, style = MaterialTheme.typography.bodySmall)
        }
    }
}

/**
 * DeleteDialog asks for the name before removing a user. The keys go with it and
 * the next apply takes the accounts off the hosts, so a second tap is too cheap.
 */
@Composable
fun DeleteDialog(
    name: String,
    accounts: Int,
    subs: Int,
    enabled: Boolean,
    busy: Boolean,
    typed: String,
    onTyped: (String) -> Unit,
    onDismiss: () -> Unit,
    onDisable: () -> Unit,
    onDelete: () -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Delete user $name?") },
        text = {
            Column {
                Text("After the next apply:")
                Text("· $accounts engine accounts and $subs subscription${if (subs == 1) "" else "s"} go away")
                Text("· secrets leave secrets.age.yaml; the UUID stays in git history")
                Text(
                    "Type the name to confirm",
                    style = MaterialTheme.typography.labelMedium,
                    modifier = Modifier.padding(top = 12.dp, bottom = 6.dp),
                )
                OutlinedTextField(
                    value = typed,
                    onValueChange = onTyped,
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        },
        confirmButton = {
            Button(onClick = onDelete, enabled = !busy && typed.trim() == name) { Text("Delete") }
        },
        dismissButton = {
            Row {
                if (enabled) {
                    TextButton(onClick = onDisable, enabled = !busy) { Text("Disable") }
                }

                TextButton(onClick = onDismiss) { Text("Cancel") }
            }
        },
    )
}

/** PlanScreen shows what apply would change on every host. */
@Composable
fun PlanScreen(ctx: android.content.Context, modifier: Modifier, onBack: () -> Unit) {
    val app = remember { Core.current() }
    var status by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var hosts by remember { mutableStateOf(emptyList<PlanHost>()) }
    var log by remember { mutableStateOf(emptyList<String>()) }

    val scope = rememberCoroutineScope()

    suspend fun load() {
        busy = true
        status = "…"
        status = onIo {
            val json = app?.plan("") ?: return@onIo "no state"
            hosts = parsePlan(json)
            "${hosts.size} hosts"
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
        Text("Plan", style = MaterialTheme.typography.headlineSmall)
        Button(onClick = onBack) { Text("Back") }
        Button(
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
            enabled = !busy,
        ) { Text("Apply") }
        Text(status, style = MaterialTheme.typography.bodySmall)

        LazyColumn {
            items(hosts) { h ->
                Column(modifier = Modifier.padding(vertical = 8.dp)) {
                    Row {
                        Text(h.name, style = MaterialTheme.typography.titleSmall)
                        Text(
                            "  ${h.changes} changes" + if (h.marker.isEmpty()) "" else " · applied ${h.marker.take(7)}",
                            style = MaterialTheme.typography.bodySmall,
                        )
                    }

                    if (!h.bootstrapped) {
                        Text("skipped: bootstrap not done", style = MaterialTheme.typography.bodySmall)
                    }

                    if (h.unreachable.isNotEmpty()) {
                        Text(h.unreachable, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
                    }

                    h.notes.forEach { note ->
                        Text(note, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.tertiary)
                    }

                    // the marks read like plan does on the command line: ~ changed, + new, - gone
                    h.files.filter { it.status != "=" }.forEach { f ->
                        Text(
                            "  ${f.status} ${f.path}",
                            style = MaterialTheme.typography.bodySmall,
                        )
                    }
                }
            }

            if (log.isNotEmpty()) {
                item {
                    Text(
                        log.takeLast(40).joinToString("\n"),
                        style = MaterialTheme.typography.bodySmall,
                        modifier = Modifier.padding(top = 8.dp),
                    )
                }
            }
        }
    }
}

const val PLAN = "@plan"
const val HOSTS = "@hosts"
const val OVERVIEW = "@overview"
const val DEVICES = "@devices"
const val ADD = "@new"
const val REPO = "git@github.com:danilrwx/vpn.git"
