package dev.vpnctl.app

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.Dispatchers
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

    when (selected) {
        null -> UsersScreen(ctx, modifier, onPick = { selected = it })
        PLAN -> PlanScreen(ctx, modifier, onBack = { selected = null })
        else -> UserScreen(ctx, modifier, name = selected!!, onBack = { selected = null })
    }
}

@Composable
fun UsersScreen(ctx: android.content.Context, modifier: Modifier, onPick: (String) -> Unit) {
    var app by remember { mutableStateOf(Core.current()) }
    var status by remember { mutableStateOf("") }

    LaunchedEffect(Unit) {
        if (app == null) {
            app = withContext(Dispatchers.IO) { Core.openBundled(ctx) }
        }

        if (app != null && !File(ctx.filesDir, "state/.git").exists()) {
            status = withContext(Dispatchers.IO) { Core.sync(ctx, REPO) }
            app = Core.current()
        }
    }

    val users = remember(app) { app?.let { parseUsers(it.users()) } ?: emptyList() }

    Column(modifier = modifier.fillMaxSize().padding(16.dp)) {
        if (status.isNotEmpty()) {
            Text(status, style = MaterialTheme.typography.bodySmall)
        }

        Text("users: ${users.size}", style = MaterialTheme.typography.titleMedium)
        Button(onClick = { onPick(PLAN) }) { Text("план") }

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
    var links by remember { mutableStateOf(emptyList<UserLink>()) }

    LaunchedEffect(name) {
        links = withContext(Dispatchers.IO) {
            app?.let { parseLinks(it.links(name)) } ?: emptyList()
        }
    }

    Column(modifier = modifier.fillMaxSize().padding(16.dp)) {
        Text(name, style = MaterialTheme.typography.headlineSmall)
        Button(onClick = onBack) { Text("назад") }

        LazyColumn(modifier = Modifier.padding(top = 8.dp)) {
            items(links) { link ->
                Text(link.label, style = MaterialTheme.typography.labelSmall)
                Text(link.url, style = MaterialTheme.typography.bodySmall)
            }
        }

        Button(onClick = { status = onIo { renewUser(app, name) } }) { Text("продлить 30 дней") }
        Button(onClick = { status = onIo { disableUser(app, name) } }) { Text("выключить") }
        Button(onClick = { status = onIo { pushState(app) } }) { Text("отправить в git") }

        if (status.isNotEmpty()) {
            Text(status, style = MaterialTheme.typography.bodySmall)
        }
    }
}

/**
 * onIo runs a call that talks to git or the disk. Compose buttons run on the UI
 * thread, and Android kills the process on a network call there.
 */
fun onIo(block: () -> String): String = kotlinx.coroutines.runBlocking(Dispatchers.IO) {
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

fun pushState(app: mobile.App?): String {
    val head = app?.push() ?: return "нет репозитория"
    return "pushed $head"
}

/** PlanScreen shows what apply would change on every host. */
@Composable
fun PlanScreen(ctx: android.content.Context, modifier: Modifier, onBack: () -> Unit) {
    val app = remember { Core.current() }
    var status by remember { mutableStateOf("") }
    var hosts by remember { mutableStateOf(emptyList<PlanHost>()) }

    LaunchedEffect(Unit) {
        status = onIo {
            val json = app?.plan("") ?: return@onIo "нет состояния"
            hosts = parsePlan(json)
            "hosts: ${hosts.size}"
        }
    }

    Column(modifier = modifier.fillMaxSize().padding(16.dp)) {
        Text("план", style = MaterialTheme.typography.headlineSmall)
        Button(onClick = onBack) { Text("назад") }
        Button(onClick = { status = onIo { applyAll(app) } }) { Text("применить") }
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
const val REPO = "git@github.com:danilrwx/vpn.git"
