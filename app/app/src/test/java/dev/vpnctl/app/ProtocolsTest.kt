package dev.vpnctl.app

import com.google.zxing.BinaryBitmap
import com.google.zxing.MultiFormatReader
import com.google.zxing.RGBLuminanceSource
import com.google.zxing.common.HybridBinarizer
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class ProtocolsTest {
    @Test
    fun `protocol names are readable without changing the core identifiers`() {
        assertEquals("Hysteria 2", protocolTitle("hy2"))
        assertEquals("AmneziaWG", protocolTitle("awg"))
        assertEquals("reality", protocolTitle("reality"))
        assertTrue(linkTitle(UserLink("split", "hy2://secret@example.com:443")).contains("Hysteria 2"))
        assertTrue(linkTitle(UserLink("full", "hysteria2://secret@example.com:443")).contains("full"))
    }

    @Test
    fun `inbound choices respect engine limitations and fill transport ports`() {
        assertTrue(inboundProtocols("sing-box", "entry").containsAll(listOf("hy2", "awg")))
        assertFalse("hy2" in inboundProtocols("xray", "entry"))
        assertFalse("awg" in inboundProtocols("xray", "entry"))
        assertTrue("awg" in inboundProtocols("xray", "exit"))
        assertEquals("443", defaultInboundPort("hy2"))
        assertEquals("51820", defaultInboundPort("awg"))
    }

    @Test
    fun `AWG profile parser preserves configuration text and metadata`() {
        val profiles = parseUserProfiles(
            """[{"name":"anna","protocol":"awg","mode":"split","filename":"anna-split-awg.conf","content":"[Interface]\nAddress = 10.8.1.2/32\nPrivateKey = secret\n"}]""",
        )
        assertEquals(1, profiles.size)
        assertEquals("awg", profiles[0].protocol)
        assertEquals("split", profiles[0].mode)
        assertEquals("anna-split-awg.conf", profiles[0].filename)
        assertEquals("[Interface]\nAddress = 10.8.1.2/32\nPrivateKey = secret\n", profiles[0].content)
        assertTrue(parseUserProfiles("[]").isEmpty())
        assertTrue(parseUserProfiles("broken").isEmpty())
        assertTrue(parseUserProfiles("""[{"filename":"missing-fields.conf"}]""").isEmpty())
    }

    @Test
    fun `profile parser rejects paths and empty configs`() {
        listOf("../secret.conf", "/tmp/key.conf", "a/b.conf", "a\\b.conf", "key.txt").forEach { path ->
            val json = org.json.JSONObject().apply {
                put("name", "anna")
                put("protocol", "awg")
                put("mode", "split")
                put("filename", path)
                put("content", "secret")
            }
            assertTrue(parseUserProfiles("[$json]").isEmpty(), path)
        }
        assertTrue(parseUserProfiles("""[{"name":"anna","protocol":"awg","mode":"split","filename":"anna.conf","content":""}]""").isEmpty())
    }

    @Test
    fun `AWG QR decodes to the exact importable client config`() {
        val config = """
            [Interface]
            Address = 10.8.1.2/32
            PrivateKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
            DNS = 10.8.1.1
            Jc = 4
            Jmin = 40
            Jmax = 70
            S1 = 0
            S2 = 0
            H1 = 1
            H2 = 2
            H3 = 3
            H4 = 4

            [Peer]
            PublicKey = BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=
            PresharedKey = CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC=
            AllowedIPs = 0.0.0.0/0, ::/0
            Endpoint = vpn.example.com:51820
            PersistentKeepalive = 25
        """.trimIndent() + "\n"
        val matrix = qrMatrix(config)
        val size = matrix.width * 4
        val pixels = IntArray(size * size) { index ->
            if (matrix[index % size / 4, index / size / 4]) 0xff000000.toInt() else 0xffffffff.toInt()
        }
        val bitmap = BinaryBitmap(HybridBinarizer(RGBLuminanceSource(size, size, pixels)))
        assertEquals(config, MultiFormatReader().decode(bitmap).text)
    }
}
