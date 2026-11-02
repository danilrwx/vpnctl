package dev.vpnctl.app

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.ListAlt
import androidx.compose.material.icons.outlined.Refresh
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
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.File
import java.time.LocalDate
import java.time.format.DateTimeFormatter
import java.time.temporal.ChronoUnit

/** When a user stops working, as the list shows it: the date and the days left. */
data class Expiry(val short: String, val days: Long?)

private val dayFormat = DateTimeFormatter.ofPattern("dd.MM")

/**
 * expiryOf reads the state's YYYY-MM-DD. A date the app cannot parse is shown as
 * it came: inventing "no expiry" for a typo would quietly keep a user alive.
 */
fun expiryOf(raw: String, today: LocalDate = LocalDate.now()): Expiry? {
    if (raw.isBlank()) {
        return null
    }

    val date = try {
        LocalDate.parse(raw.trim())
    } catch (e: Exception) {
        return Expiry(raw.trim(), null)
    }

    return Expiry(date.format(dayFormat), ChronoUnit.DAYS.between(today, date))
}

/** expiryState is the one word the list shows under the right-hand date. */
fun expiryState(expiry: Expiry?, enabled: Boolean): Pair<String, ExpiryTone> = when {
    !enabled -> "disabled" to ExpiryTone.Bad
    expiry == null -> "no expiry" to ExpiryTone.Muted
    expiry.days == null -> expiry.short to ExpiryTone.Muted
    expiry.days < 0 -> "expired" to ExpiryTone.Bad
    expiry.days == 0L -> "today" to ExpiryTone.Warn
    expiry.days <= 14 -> "in ${expiry.days} d" to ExpiryTone.Warn
    else -> "in ${expiry.days} d" to ExpiryTone.Muted
}

enum class ExpiryTone { Muted, Warn, Bad }

/**
 * UsersScreen is the list from the mockup: a search pill, the four filters with
 * their counts, and one row per person. Traffic and "last seen" are not in the
 * core, so the right column carries the two facts that are: the date and the
 * days left (DESIGN.md correction 1).
 */
@Composable
fun UsersScreen(
    ctx: android.content.Context,
    modifier: Modifier,
    version: Int,
    onPick: (String) -> Unit,
    onNew: () -> Unit,
    onPlan: () -> Unit,
) {
    var status by remember { mutableStateOf("") }
    // The handle is read in composition, not kept in state: a Go object parked in
    // Compose state did not survive the open (the write never reached the read),
    // and the list stayed on the APK fixture until the process restarted.
    var epoch by remember { mutableStateOf(0) }
    var query by remember { mutableStateOf("") }
    var filter by remember { mutableStateOf("All") }

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

    val active = users.count { it.enabled }
    val expiring = users.count { u ->
        val e = expiryOf(u.expires)
        u.enabled && e?.days != null && e.days in 0..14
    }
    val disabled = users.count { !it.enabled }

    val visible = users.filter { user ->
        val matches = query.isBlank() || user.name.contains(query.trim(), ignoreCase = true)
        val e = expiryOf(user.expires)
        val inFilter = when (filter) {
            "Active" -> user.enabled
            "Expiring" -> user.enabled && e?.days != null && e.days in 0..14
            "Disabled" -> !user.enabled
            else -> true
        }

        matches && inFilter
    }.sortedBy { it.name }

    Box(modifier = modifier.fillMaxSize()) {
        Column(modifier = Modifier.fillMaxSize()) {
            ScreenHeader(
                title = "Users",
                subtitle = "${users.size} · $active active",
                actions = {
                    IconAction(Icons.Outlined.Refresh, { epoch++ }, "Reload")
                    IconAction(Icons.Outlined.ListAlt, onPlan, "Plan")
                },
            )

            SearchPill(query, { query = it }, "Search users")

            Spacer(Modifier.height(12.dp))

            LazyRow(
                horizontalArrangement = androidx.compose.foundation.layout.Arrangement.spacedBy(8.dp),
                contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = 16.dp),
            ) {
                item { Chip("All ${users.size}", { filter = "All" }, selected = filter == "All", check = true) }
                item { Chip("Active $active", { filter = "Active" }, selected = filter == "Active") }
                item { Chip("Expiring $expiring", { filter = "Expiring" }, selected = filter == "Expiring", warn = expiring > 0) }
                item { Chip("Disabled $disabled", { filter = "Disabled" }, selected = filter == "Disabled") }
            }

            if (status.isNotEmpty()) {
                Text(
                    status,
                    style = MaterialTheme.typography.bodySmall,
                    color = if (status.startsWith("not connected")) Warn else Muted,
                    modifier = Modifier.padding(horizontal = 16.dp, vertical = 6.dp),
                )
            }

            LazyColumn(modifier = Modifier.fillMaxSize()) {
                items(visible, key = { it.name }) { user ->
                    UserListRow(user, onClick = { onPick(user.name) })
                }

                if (visible.isEmpty()) {
                    item {
                        Text(
                            if (users.isEmpty()) "no users in the state" else "nothing matches",
                            style = MaterialTheme.typography.bodyMedium,
                            color = Muted,
                            modifier = Modifier.padding(16.dp),
                        )
                    }
                }

                // the FAB floats over the list; the last row must not hide under it
                item { Spacer(Modifier.height(88.dp)) }
            }
        }

        Box(modifier = Modifier.align(Alignment.BottomEnd).padding(end = 18.dp, bottom = 18.dp)) {
            RoundFab(onClick = onNew, icon = Icons.Outlined.Add, description = "New user")
        }
    }
}

@Composable
fun UserListRow(user: UserRow, onClick: () -> Unit) {
    val expiry = expiryOf(user.expires)
    val (state, tone) = expiryState(expiry, user.enabled)
    val toneColor = when (tone) {
        ExpiryTone.Muted -> Muted
        ExpiryTone.Warn -> Warn
        ExpiryTone.Bad -> DangerText
    }

    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(horizontal = 16.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Avatar(user.name, modifier = Modifier)

        Spacer(Modifier.width(14.dp))

        Column(modifier = Modifier.weight(1f)) {
            Text(user.name, style = MaterialTheme.typography.titleLarge, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(
                buildAnnotatedString {
                    append(user.modes)

                    if (user.tg) {
                        append(" · tg")
                    }

                    if (!user.enabled) {
                        withStyle(SpanStyle(color = DangerText)) { append(" · disabled") }
                    }
                },
                style = MaterialTheme.typography.bodySmall,
                color = Muted,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.padding(top = 3.dp),
            )
        }

        Column(horizontalAlignment = Alignment.End) {
            MonoText(
                expiry?.short ?: "—",
                color = if (user.enabled) Ink else Muted,
                style = MaterialTheme.typography.bodyMedium,
            )
            Text(
                state,
                style = MaterialTheme.typography.labelMedium,
                color = toneColor,
                modifier = Modifier.padding(top = 3.dp),
            )
        }
    }
}
