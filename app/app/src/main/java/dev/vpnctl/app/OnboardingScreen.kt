package dev.vpnctl.app

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import java.io.File

/**
 * OnboardingScreen is the first launch: the app has no state until it is pointed
 * at a repository, and the phone has to hand the operator its two public keys
 * before the repository will accept it.
 *
 * It is deliberately short: the two keys, the repository, done. The drawings walk
 * through bootstrap and a second device; both of those run on the command line,
 * so the screen says where they happen instead of drawing steps it cannot take.
 */
@Composable
fun OnboardingScreen(ctx: android.content.Context, onDone: () -> Unit) {
    val scope = rememberCoroutineScope()

    var step by remember { mutableStateOf(0) }
    var repo by remember { mutableStateOf(Prefs.repo(ctx)) }
    var status by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var head by remember { mutableStateOf("") }
    var users by remember { mutableStateOf(0) }

    val recipient = remember { localRecipient(ctx) }
    val sshKey = remember { Core.identityPEM(ctx) }

    fun connect() {
        if (busy || repo.isBlank()) {
            return
        }

        busy = true
        status = "…"

        scope.launch {
            Prefs.setRepo(ctx, repo)
            status = onIo { Core.connect(ctx, repo) }
            busy = false

            if (status.startsWith("synced at")) {
                head = status.removePrefix("synced at ").trim()
                users = Core.current()?.let { parseUsers(it.users()).size } ?: 0
                step = 1
            }
        }
    }

    Column(modifier = Modifier.fillMaxSize()) {
        ScreenHeader(title = "vpnctl", subtitle = if (step == 0) "1 of 2 · the keys" else "2 of 2 · the state")

        LazyColumn(modifier = Modifier.weight(1f)) {
            if (step == 0) {
                item {
                    SurfaceCard(modifier = Modifier.padding(16.dp)) {
                        Column(modifier = Modifier.fillMaxWidth().padding(16.dp)) {
                            Text("This phone needs two keys", style = MaterialTheme.typography.titleMedium)
                            Text(
                                "The age key decrypts the secrets; the ssh key signs what the hosts execute. Neither leaves the Keystore.",
                                style = MaterialTheme.typography.bodySmall,
                                color = Muted,
                                modifier = Modifier.padding(top = 4.dp),
                            )

                            Text("age recipient", style = MaterialTheme.typography.labelMedium, color = Muted, modifier = Modifier.padding(top = 12.dp))
                            MonoText(recipient.ifEmpty { "none yet" }, color = Link, style = MaterialTheme.typography.labelMedium, maxLines = 2)
                            TextButton(onClick = { copyToClipboard(ctx, recipient) }, enabled = recipient.isNotEmpty()) {
                                Text("Copy recipient", color = Link, style = MaterialTheme.typography.bodySmall)
                            }

                            Text("ssh key", style = MaterialTheme.typography.labelMedium, color = Muted)
                            Text(
                                if (sshKey.isEmpty()) "none yet" else "ecdsa P-256 · held by the Keystore",
                                style = MaterialTheme.typography.bodySmall,
                                color = Link,
                            )
                            TextButton(onClick = { copyToClipboard(ctx, sshKey) }, enabled = sshKey.isNotEmpty()) {
                                Text("Copy key", color = Link, style = MaterialTheme.typography.bodySmall)
                            }

                            Text(
                                "Add the recipient with `vpnctl key add phone <age1…>` and the ssh key with `vpnctl host agent-install` from the laptop. Both are one-time.",
                                style = MaterialTheme.typography.labelMedium,
                                color = Muted,
                                modifier = Modifier.padding(top = 8.dp),
                            )
                        }
                    }
                }

                item {
                    SectionLabel("State repository")
                    SurfaceCard(modifier = Modifier.padding(horizontal = 16.dp)) {
                        Column(modifier = Modifier.fillMaxWidth().padding(16.dp)) {
                            Text(
                                "the phone clones it with its own deploy key",
                                style = MaterialTheme.typography.bodySmall,
                                color = Muted,
                            )
                            OutlinedTextField(
                                value = repo,
                                onValueChange = { repo = it },
                                label = { Text("git@github.com:owner/state.git") },
                                singleLine = true,
                                textStyle = MaterialTheme.typography.bodyMedium,
                                colors = appFieldColors(),
                                modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
                            )
                        }
                    }
                }

                item {
                    Column(modifier = Modifier.fillMaxWidth().padding(16.dp)) {
                        PrimaryButton(
                            "Connect",
                            onClick = { connect() },
                            enabled = !busy && repo.isNotBlank(),
                            modifier = Modifier.fillMaxWidth(),
                        )
                        TextButton(onClick = onDone, modifier = Modifier.fillMaxWidth()) {
                            Text("Skip · look at the bundled fixture", color = Muted, style = MaterialTheme.typography.bodySmall)
                        }
                    }
                }
            } else {
                item {
                    SurfaceCard(modifier = Modifier.padding(16.dp)) {
                        Column(modifier = Modifier.fillMaxWidth().padding(16.dp)) {
                            Text("The state is here", style = MaterialTheme.typography.titleMedium)
                            Text(
                                "synced at $head · $users ${plural(users, "user", "users")}",
                                style = MaterialTheme.typography.bodySmall,
                                color = Muted,
                                modifier = Modifier.padding(top = 4.dp),
                            )
                            Text(
                                "The hosts get the changes on the next apply, and apply is on the Plan screen. A second device is added from Devices once it has its own key.",
                                style = MaterialTheme.typography.bodySmall,
                                color = Muted,
                                modifier = Modifier.padding(top = 8.dp),
                            )
                        }
                    }
                }

                item {
                    Column(modifier = Modifier.fillMaxWidth().padding(16.dp)) {
                        PrimaryButton("Go to the estate", onClick = onDone, modifier = Modifier.fillMaxWidth())
                    }
                }
            }
        }

        if (status.isNotEmpty() && !status.startsWith("synced at")) {
            Text(
                status,
                style = MaterialTheme.typography.bodySmall,
                color = if (status.startsWith("error") || status.startsWith("sync")) DangerText else Muted,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
            )
        }

        Spacer(Modifier.height(8.dp))
    }
}

/** needsOnboarding is the first launch: no clone and nowhere to get one. */
fun needsOnboarding(ctx: android.content.Context): Boolean =
    Prefs.repo(ctx).isEmpty() && !File(ctx.filesDir, "state/.git").exists()
