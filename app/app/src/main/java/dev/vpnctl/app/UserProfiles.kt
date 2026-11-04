package dev.vpnctl.app

import android.content.ClipData
import android.content.Context
import android.content.Intent
import androidx.core.content.FileProvider
import org.json.JSONArray
import java.io.File

data class UserProfile(
    val name: String,
    val protocol: String,
    val mode: String,
    val filename: String,
    val content: String,
)

/** Configs contain private keys. Keep them separate from copyable public URLs. */
fun parseUserProfiles(json: String): List<UserProfile> = try {
    val arr = JSONArray(json)
    (0 until arr.length()).map { i ->
        val o = arr.getJSONObject(i)
        UserProfile(
            o.getString("name"), o.getString("protocol"), o.getString("mode"),
            o.getString("filename"), o.getString("content"),
        )
    }.filter { it.content.isNotBlank() && it.filename.matches(Regex("[a-zA-Z0-9_.-]+\\.conf")) }
} catch (e: Exception) {
    emptyList()
}

/** Share a real .conf attachment with a temporary read grant, never a public file. */
fun shareProfile(ctx: Context, profile: UserProfile) {
    require(profile.filename.matches(Regex("[a-zA-Z0-9_.-]+\\.conf")))
    val dir = File(ctx.cacheDir, "profiles").apply { mkdirs() }
    val file = File(dir, profile.filename)
    file.writeText(profile.content)
    val uri = FileProvider.getUriForFile(ctx, "${ctx.packageName}.profiles", file)
    val send = Intent(Intent.ACTION_SEND).apply {
        type = "application/octet-stream"
        putExtra(Intent.EXTRA_STREAM, uri)
        putExtra(Intent.EXTRA_SUBJECT, profile.filename)
        clipData = ClipData.newRawUri(profile.filename, uri)
        addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
    }
    ctx.startActivity(Intent.createChooser(send, "Share ${profile.filename}"))
}
