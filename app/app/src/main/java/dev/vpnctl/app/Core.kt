package dev.vpnctl.app

import android.content.Context
import java.io.File
import mobile.App
import mobile.Mobile

/**
 * The Go core, opened once per process. The state is copied out of the APK on
 * first launch: the emulator has nothing else, and a phone gets the real
 * repository from the sync screen later.
 */
object Core {
    private var app: App? = null

    fun open(dir: String, ageKeyPath: String): App? {
        app?.let { return it }

        return try {
            Mobile.open(dir, ageKeyPath).also { app = it }
        } catch (e: Exception) {
            null
        }
    }

    /** openBundled copies the fixture out of assets and opens it. */
    fun openBundled(ctx: Context): App? {
        val dir = File(ctx.filesDir, "state")
        if (!dir.exists()) {
            copyAssets(ctx, "state", dir)
        }

        return open(dir.absolutePath, "")
    }

    private fun copyAssets(ctx: Context, from: String, to: File) {
        for (name in ctx.assets.list(from).orEmpty()) {
            val path = "$from/$name"
            if (ctx.assets.list(path).orEmpty().isNotEmpty()) {
                copyAssets(ctx, path, File(to, name))
                continue
            }

            val out = File(to, name)
            out.parentFile?.mkdirs()
            ctx.assets.open(path).use { input -> out.outputStream().use { input.copyTo(it) } }
        }
    }

    /** signedBy is what the host would receive: the device's public key. */
    fun signedBy(): String = try {
        val signer = KeystoreSigner().ensure()
        Mobile.setSigner(signer)
        Mobile.signedBy()
    } catch (e: Exception) {
        "no signer: ${e.message}"
    }

    fun current(): App? = app
}
