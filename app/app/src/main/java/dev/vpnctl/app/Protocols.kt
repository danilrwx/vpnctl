package dev.vpnctl.app

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp

fun protocolTitle(protocol: String): String = when (protocol) {
    "hy2" -> "Hysteria 2"
    "awg" -> "AmneziaWG"
    else -> protocol
}

/** Wrap rather than clip protocol names on small phones and at large font sizes. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun ProtocolChoices(
    protocols: List<String>,
    selected: Set<String>,
    modifier: Modifier = Modifier,
    onSelect: (String) -> Unit,
) {
    FlowRow(
        modifier = modifier,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        protocols.forEach { protocol ->
            Chip(protocolTitle(protocol), { onSelect(protocol) }, selected = protocol in selected)
        }
    }
}

/** Xray cannot serve HY2 or the entry's L3 DNS. */
fun inboundProtocols(engine: String, role: String): List<String> =
    listOf("reality", "ws", "hy2", "awg", "mtg").filterNot {
        engine == "xray" && (it == "hy2" || (it == "awg" && role == "entry"))
    }

fun defaultInboundPort(protocol: String): String = when (protocol) {
    "reality", "hy2" -> "443"
    "ws" -> "8444"
    "awg" -> "51820"
    "mtg" -> "2053"
    else -> ""
}
