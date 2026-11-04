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
    // The app is opened on a worker thread and read on the main one, so the field
    // has to be visible across them: without this a fresh clone stayed invisible
    // to the first screen until the process was restarted.
    @Volatile
    private var app: App? = null

    fun open(dir: String, ageKeyPath: String): App? {
        app?.let { return it }

        return try {
            Mobile.open(dir, ageKeyPath).also { app = it }
        } catch (e: Exception) {
            null
        }
    }

    /**
     * ageKeyFile unwraps the identity into a file the Go side can read. It has
     * to exist as a file: age takes a path, not bytes, and the app keeps it
     * owner-only. It is written on every launch and lives as long as the process.
     */
    fun ageKeyFile(ctx: Context): String? {
        val secret = Vault(ctx.filesDir).get("age.key") ?: return null

        val file = File(ctx.filesDir, "age.key.txt")
        file.writeText("# vpnctl device identity\n$secret\n")
        file.setReadable(false, false)
        file.setReadable(true, true)
        file.setWritable(false, false)
        file.setWritable(true, true)

        return file.absolutePath
    }

    /**
     * openBundled opens the cloned state when there is one and the fixture that
     * ships in the APK otherwise. The fixture is the only way to see anything on
     * a fresh install; a cloned repository always wins.
     */
    fun openBundled(ctx: Context): App? {
        val dir = File(ctx.filesDir, "state")

        if (File(dir, ".git").exists()) {
            return open(dir.absolutePath, ageKeyFile(ctx) ?: "")
        }

        if (!dir.exists()) {
            copyAssets(ctx, "state", dir)
        }

        // Creating a demo user also writes encrypted protocol credentials.
        // Use the phone's identity, just as the cloned state does.
        return open(dir.absolutePath, ageKeyFile(ctx) ?: "")
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

    /**
     * identityKey is the device key as an authorized_keys line, which is also
     * what GitHub's deploy key field accepts. It is written to
     * filesDir/identity.pub: the phone has to hand it to a host or a repository,
     * and copying from a screen is the worst way to do that.
     */
    fun identityKey(ctx: Context): String {
        val key = sshKey()
        File(ctx.filesDir, "identity.pub").writeText(key)
        return key
    }

    /**
     * installSigner wires the Keystore key into the core. Everything that signs
     * depends on it: agent requests and git alike, so it happens once at startup
     * and not on the first screen that happens to need it.
     */
    fun installSigner(ctx: Context) {
        try {
            Mobile.setSigner(KeystoreSigner().ensure())
            // the public half is written every start: a reinstall changes the
            // key, and a stale file here is a key the host does not know
            File(ctx.filesDir, "identity.pub").writeText(Mobile.authorizedKey())
        } catch (e: Exception) {
            // a device without a Keystore key can still read the state; it just
            // cannot sign, and the screens that need a signature say so
        }
    }

    /** sshKey is the device's public key in the form sshd and GitHub read. */
    fun sshKey(): String = try {
        Mobile.setSigner(KeystoreSigner().ensure())
        Mobile.authorizedKey()
    } catch (e: Exception) {
        "no signer: ${e.message}"
    }

    /** signSelfTest signs a request with the Keystore key and verifies it. */
    fun signSelfTest(): String = try {
        Mobile.selfTest()
    } catch (e: Exception) {
        "self-test: ${e.message}"
    }

    /**
     * identity returns the device's age recipient, minting the pair on first
     * launch. The secret is stored under the Keystore vault; the recipient is
     * public and goes into age.yaml with `vpnctl key add phone <age1…>`.
     */
    fun identity(ctx: Context): String = try {
        val recipient = File(ctx.filesDir, "age.recipient")

        if (recipient.exists()) {
            recipient.readText().trim()
        } else {
            val fresh = Mobile.newIdentity()
            val check = Mobile.roundTrip(fresh.recipient, fresh.secret)

            if (check != "identity works") {
                "identity: $check"
            } else {
                Vault(ctx.filesDir).put("age.key", fresh.secret)
                recipient.writeText(fresh.recipient)
                fresh.recipient
            }
        }
    } catch (e: Exception) {
        "identity: ${e.message}"
    }

    /**
     * registerDevice writes this phone into devices.yaml: its OpenSSH key and
     * its age recipient. Push publishes the commit; the laptop's device sync
     * adds the recipient to age.yaml and apply puts the key on the hosts.
     */
    fun registerDevice(ctx: Context, name: String): String = try {
        val app = current() ?: return "no state to register in"
        app.registerDevice(name, identity(ctx))
    } catch (e: Exception) {
        "register: ${e.message}"
    }

    /**
     * sync clones the state repository with the device key. The first call takes
     * a while and must not run on the UI thread; the caller does that part.
     */
    fun sync(ctx: Context, url: String): String = try {
        val dir = File(ctx.filesDir, "state")
        if (File(dir, ".git").exists()) {
            "already cloned"
        } else {
            // The fixture that ships in the APK is copied to the same path, so a
            // clone into it would leave the fixture's files in a repository that
            // reports the real head: the app then shows both, and the timestamps
            // make it look like a successful sync.
            if (dir.exists()) {
                dir.deleteRecursively()
            }

            val head = Mobile.syncState(url, dir.absolutePath)
            app = null
            open(dir.absolutePath, ageKeyFile(ctx) ?: "")
            "synced at $head"
        }
    } catch (e: Exception) {
        "sync: ${e.message}"
    }

    /**
     * connect replaces the local copy with a clone of url. The fixture and any
     * previous clone go first: a repository that already exists would make sync
     * answer "already cloned" and silently keep the old state.
     */
    fun connect(ctx: Context, url: String): String {
        val dir = File(ctx.filesDir, "state")

        if (dir.exists()) {
            dir.deleteRecursively()
        }

        app = null

        return sync(ctx, url)
    }

    fun current(): App? = app

    /**
     * reopen drops the opened state and opens it again. A restored identity
     * replaces the key file the core read at startup, so the handle it holds
     * points at the old key until this runs.
     */
    fun reopen(ctx: Context): App? {
        app = null

        val dir = File(ctx.filesDir, "state")
        if (!dir.exists()) {
            return null
        }

        return open(dir.absolutePath, ageKeyFile(ctx) ?: "")
    }
}
