package dev.vpnctl.app

/**
 * ProgressSink forwards apply's lines to the screen. The call is long — several
 * hosts, each with a signed request, a probe and a marker — and a single spinner
 * for all of it is indistinguishable from a hang.
 */
class ProgressSink(private val onLine: (String) -> Unit) : mobile.Progress {
    override fun update(line: String) {
        onLine(line)
    }
}

/** applyWithProgress is apply, with the same lines the command line prints. */
fun applyWithProgress(app: mobile.App?, host: String, onLine: (String) -> Unit): String {
    val result = app?.applyProgress(host, ProgressSink(onLine)) ?: return "no state"

    return if (result.contains("\"failed\"")) "error: $result" else "applied"
}
