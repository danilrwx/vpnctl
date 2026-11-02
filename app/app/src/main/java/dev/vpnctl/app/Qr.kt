package dev.vpnctl.app

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import com.google.zxing.BarcodeFormat
import com.google.zxing.EncodeHintType
import com.google.zxing.common.BitMatrix
import com.google.zxing.qrcode.QRCodeWriter
import com.google.zxing.qrcode.decoder.ErrorCorrectionLevel
import kotlin.math.ceil
import kotlin.math.floor

/** QrDialog shows a link as a QR: the phone is where a link is handed over. */
@Composable
fun QrDialog(label: String, url: String, onDismiss: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        shape = RoundedCornerShape(24.dp),
        containerColor = Surface2,
        titleContentColor = Ink,
        title = { Text(label, style = MaterialTheme.typography.titleMedium) },
        text = {
            Column(horizontalAlignment = Alignment.CenterHorizontally) {
                Box(
                    modifier = Modifier
                        .clip(RoundedCornerShape(14.dp))
                        .background(Color.White)
                        .padding(12.dp),
                ) {
                    QrCode(url, 220.dp)
                }

                MonoText(
                    url,
                    color = Link,
                    style = MaterialTheme.typography.bodySmall,
                    maxLines = 2,
                    modifier = Modifier.padding(top = 12.dp),
                )
            }
        },
        confirmButton = {
            TextButton(onClick = onDismiss) { Text("Close", color = Link) }
        },
    )
}

/** QrCode draws the matrix the encoder returns, one square per module. */
@Composable
fun QrCode(text: String, size: Dp) {
    val matrix = remember(text) { qrMatrix(text) }

    androidx.compose.foundation.Canvas(modifier = Modifier.size(size)) {
        val n = matrix.width
        val step = this.size.width / n

        for (x in 0 until n) {
            for (y in 0 until n) {
                if (!matrix.get(x, y)) {
                    continue
                }

                // Snap the edges outward: rounding each module independently
                // leaves hairline seams that a scanner reads as white.
                val left = floor(x * step)
                val top = floor(y * step)

                drawRect(Color.Black, Offset(left, top), Size(ceil((x + 1) * step) - left, ceil((y + 1) * step) - top))
            }
        }
    }
}

/** qrMatrix asks for one pixel per module (0 means "the natural size"): asking
 * for a big matrix makes the encoder scale it up, and then the canvas draws a
 * rectangle per *pixel* — 660² of them, which is an ANR, not a QR code. */
fun qrMatrix(text: String): BitMatrix = QRCodeWriter().encode(
    text,
    BarcodeFormat.QR_CODE,
    0,
    0,
    mapOf<EncodeHintType, Any>(EncodeHintType.MARGIN to 1, EncodeHintType.ERROR_CORRECTION to ErrorCorrectionLevel.M),
)
