package dev.vpnctl.app

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import org.json.JSONArray

/** One row of the list screen. */
data class UserRow(val name: String, val modes: String, val enabled: Boolean)

/**
 * The first screen is the list of users: it is what an operator opens the app
 * for, and it proves the Go core answers before any navigation exists.
 */
class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        // the fixture ships inside the APK until the sync screen exists
        Core.openBundled(applicationContext)
        setContent {
            MaterialTheme {
                Scaffold { padding ->
                    Users(modifier = Modifier.padding(padding))
                }
            }
        }
    }
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
            // JSONArray is not an Iterable: the list is built by index
            modes = (0 until modes.length()).joinToString("/") { modes.getString(it) },
            enabled = o.getBoolean("enabled"),
        )
    }
} catch (e: Exception) {
    emptyList()
}

@Composable
fun Users(modifier: Modifier = Modifier) {
    val ctx = LocalContext.current
    val app = Core.current()
    val users = remember(app) { app?.let { parseUsers(it.users()) } ?: emptyList() }

    Column(modifier = modifier.fillMaxSize().padding(16.dp)) {
        if (app == null) {
            Text("state is not open yet")
            return@Column
        }
        Text("users: ${users.size}", style = MaterialTheme.typography.titleMedium)
        // the key has to reach the host, so it is shown whole and selectable
        SelectionContainer {
            Text(
                Core.identityPEM(ctx),
                style = MaterialTheme.typography.bodySmall,
                fontFamily = FontFamily.Monospace,
            )
        }
        // the first proof that the Keystore key crosses into Go the way the
        // agent expects: a PKIX PEM inside the signature envelope
        Text(Core.signedBy().lineSequence().firstOrNull { it.isNotBlank() } ?: "", style = MaterialTheme.typography.bodySmall)
        // the key is only useful if the host would accept a signature made with
        // it: this runs the same verifier the host runs
        Text(Core.signSelfTest(), style = MaterialTheme.typography.bodySmall)
        LazyColumn {
            items(users) { user ->
                val state = if (user.enabled) "" else " · off"
                Text("${user.name}$state  ${user.modes}")
            }
        }
    }
}
