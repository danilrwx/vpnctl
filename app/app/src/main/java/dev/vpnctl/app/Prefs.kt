package dev.vpnctl.app

import android.content.Context

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
