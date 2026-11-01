package dev.vpnctl.app

import androidx.compose.runtime.remember
import mobile.App
import mobile.Mobile

/**
 * The Go core, opened once per process. The state lives in the app's private
 * storage; until the sync screen exists the app opens the directory it was
 * given at build time and says so when there is none.
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

    fun current(): App? = app
}

@androidx.compose.runtime.Composable
fun rememberApp(): App? = remember { Core.current() }
