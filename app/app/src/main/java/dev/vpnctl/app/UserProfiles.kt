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

private val profileFilename = Regex("[a-zA-Z0-9_.-]+\\.(conf|yaml)")

/** Parse client configuration attachments and their filenames. */
fun parseUserProfiles(json: String): List<UserProfile> = try {
    val arr = JSONArray(json)
    (0 until arr.length()).map { i ->
        val o = arr.getJSONObject(i)
        UserProfile(
            o.getString("name"), o.getString("protocol"), o.getString("mode"),
            o.getString("filename"), o.getString("content"),
        )
    }.filter { it.content.isNotBlank() && it.filename.matches(profileFilename) }
} catch (e: Exception) {
    emptyList()
}

/** Share a client config attachment with a temporary read grant. */
fun shareProfile(ctx: Context, profile: UserProfile) {
    require(profile.filename.matches(profileFilename))
    val dir = File(ctx.cacheDir, "profiles").apply { mkdirs() }
    val file = File(dir, profile.filename)
    file.writeText(profile.content)
    val uri = FileProvider.getUriForFile(ctx, "${ctx.packageName}.profiles", file)
    val send = Intent(Intent.ACTION_SEND).apply {
        type = if (profile.filename.endsWith(".yaml")) "application/yaml" else "application/octet-stream"
        putExtra(Intent.EXTRA_STREAM, uri)
        putExtra(Intent.EXTRA_SUBJECT, profile.filename)
        clipData = ClipData.newRawUri(profile.filename, uri)
        addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
    }
    ctx.startActivity(Intent.createChooser(send, "Share ${profile.filename}"))
}
