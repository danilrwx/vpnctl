package dev.vpnctl.app

import android.content.Context

/**
 * normalizeRepo turns what a person pastes into the ssh remote the app's git
 * client uses. `owner/repo`, `github.com/owner/repo` and
 * `https://github.com/owner/repo` all become `git@github.com:owner/repo.git`;
 * an ssh remote is left alone.
 */
fun normalizeRepo(input: String): String {
    val raw = input.trim()
    if (raw.isEmpty()) return ""
    if (raw.startsWith("git@") || raw.startsWith("ssh://")) return raw

    var s = raw.removePrefix("https://").removePrefix("http://")
    s = s.removePrefix("www.github.com/").removePrefix("github.com/")
    s = s.trimEnd('/')
    s = s.removeSuffix(".git")

    val parts = s.split('/').filter { it.isNotEmpty() }
    return if (parts.size == 2) "git@github.com:${parts[0]}/${parts[1]}.git" else raw
}

/** Prefs keeps the one setting the app has: where the state lives. */
object Prefs {
    private const val FILE = "vpnctl"
    private const val REPO = "repo"

    fun repo(ctx: Context): String =
        ctx.getSharedPreferences(FILE, Context.MODE_PRIVATE).getString(REPO, "").orEmpty()

    fun setRepo(ctx: Context, url: String) {
        ctx.getSharedPreferences(FILE, Context.MODE_PRIVATE).edit().putString(REPO, url.trim()).apply()
    }
}
