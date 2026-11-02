package dev.vpnctl.app

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AltRoute
import androidx.compose.material.icons.outlined.Dashboard
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.Hub
import androidx.compose.material.icons.outlined.People
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
import androidx.core.view.WindowCompat
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

        // the app draws behind the system bars: the mockups are edge to edge and
        // the canvas is the background, so the icons have to stay light
        WindowCompat.getInsetsController(window, window.decorView).apply {
            isAppearanceLightStatusBars = false
            isAppearanceLightNavigationBars = false
        }

        setContent {
            VpnctlTheme {
                App()
            }
        }
    }
}

/** The five surfaces of the mockups, in their order. Devices is not a tab: it
 * is one screen about this installation, opened from the Overview header. */
val TABS = listOf(
    NavItem("Overview", Icons.Outlined.Dashboard),
    NavItem("Users", Icons.Outlined.People),
    NavItem("Hosts", Icons.Outlined.Dns),
    NavItem("Topology", Icons.Outlined.Hub),
    NavItem("Routing", Icons.Outlined.AltRoute),
)

@Composable
fun App(modifier: Modifier = Modifier) {
    val ctx = androidx.compose.ui.platform.LocalContext.current
    var tab by remember { mutableStateOf(1) }
    // a new user is written by another screen; the list reads it again when this
    // changes, so the count and the rows cannot go stale
    var version by remember { mutableStateOf(0) }
    var user by remember { mutableStateOf<String?>(null) }
    var adding by remember { mutableStateOf(false) }
    var planOpen by remember { mutableStateOf(false) }
    var devicesOpen by remember { mutableStateOf(false) }
    var hostOpen by remember { mutableStateOf<String?>(null) }
    var hostAddOpen by remember { mutableStateOf(false) }
    var setsOpen by remember { mutableStateOf(false) }
    // the first launch has nothing to show: point it at a repository before the
    // tabs make sense, and let it be skipped to look at the fixture
    var onboarding by remember { mutableStateOf(needsOnboarding(ctx)) }

    val overlay = user != null || adding || planOpen || devicesOpen || hostOpen != null || hostAddOpen || setsOpen

    if (onboarding) {
        Scaffold(containerColor = Canvas, contentWindowInsets = WindowInsets.safeDrawing) { padding ->
            Box(modifier = Modifier.fillMaxSize().padding(padding)) {
                OnboardingScreen(ctx, onDone = { onboarding = false })
            }
        }

        return
    }

    // The card, the plan and the new-user form are overlays over one activity:
    // without this, the system Back finishes the app instead of closing them.
    BackHandler(enabled = overlay) {
        user = null
        adding = false
        planOpen = false
        devicesOpen = false
        hostOpen = null
        hostAddOpen = false
        setsOpen = false
    }

    Scaffold(
        modifier = modifier.fillMaxSize(),
        containerColor = Canvas,
        contentWindowInsets = WindowInsets.safeDrawing,
        bottomBar = {
            if (!overlay) {
                Box(modifier = Modifier.navigationBarsPadding()) {
                    BottomNav(TABS, tab) { tab = it }
                }
            }
        },
    ) { padding ->
        Box(modifier = Modifier.fillMaxSize().padding(padding)) {
            when {
                planOpen -> PlanScreen(ctx, Modifier, onBack = { planOpen = false })
                devicesOpen -> DevicesScreen(ctx, Modifier, onBack = { devicesOpen = false })
                hostOpen != null -> HostScreen(ctx, Modifier, name = hostOpen!!, onBack = { hostOpen = null })
                setsOpen -> SetsScreen(ctx, Modifier, onBack = { setsOpen = false })
                hostAddOpen -> AddHostScreen(
                    ctx,
                    Modifier,
                    onBack = { hostAddOpen = false },
                    onCreated = {
                        hostAddOpen = false
                        tab = 2
                    },
                )
                adding -> AddUserScreen(
                    ctx,
                    Modifier,
                    onBack = { adding = false },
                    onCreated = {
                        version++
                        adding = false
                    },
                )
                user != null -> UserScreen(
                    ctx,
                    Modifier,
                    name = user!!,
                    onBack = { user = null },
                    onDeleted = {
                        version++
                        user = null
                    },
                )
                tab == 0 -> OverviewScreen(
                    ctx,
                    Modifier,
                    onBack = { tab = 1 },
                    onPlan = { planOpen = true },
                    onDevices = { devicesOpen = true },
                )
                tab == 1 -> UsersScreen(
                    ctx,
                    Modifier,
                    version,
                    onPick = { user = it },
                    onNew = { adding = true },
                    onPlan = { planOpen = true },
                )
                tab == 2 -> HostsScreen(
                    ctx,
                    Modifier,
                    onBack = { tab = 1 },
                    onPick = { hostOpen = it },
                    onAddHost = { hostAddOpen = true },
                )
                tab == 3 -> TopologyScreen(ctx, Modifier, onBack = { tab = 1 })
                else -> RoutingScreen(ctx, Modifier, onBack = { tab = 1 }, onSets = { setsOpen = true })
            }
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
fun addUser(
    app: mobile.App?,
    name: String,
    modes: Set<String>,
    protocols: Set<String>,
    tg: Boolean,
    expires: String,
): String {
    val msg = app?.addUser(name, modes.joinToString(","), protocols.joinToString(","), tg, expires)
        ?: return "no state"
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

const val REPO = "git@github.com:danilrwx/vpn.git"
