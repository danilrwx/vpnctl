package dev.vpnctl.app

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

/**
 * The Go core answers with JSON because gomobile cannot bind a struct with a
 * slice field; these tests pin the parse on the Kotlin side, where a wrong field
 * name would otherwise show as an empty list and nothing else.
 */
class UsersTest {
    @Test
    fun `parses what the core returns`() {
        val json = """
            [
              {"name":"brat","modes":["split","full"],"protocols":["reality"],"tg":true,"enabled":true},
              {"name":"olga","modes":["split"],"protocols":["reality"],"tg":false,"enabled":false}
            ]
        """.trimIndent()

        val users = parseUsers(json)

        assertEquals(2, users.size)
        assertEquals("brat", users[0].name)
        assertEquals("split/full", users[0].modes)
        assertTrue(users[0].enabled)
        assertTrue(!users[1].enabled)
    }

    @Test
    fun `a broken answer is an empty list, not a crash`() {
        assertTrue(parseUsers("not json").isEmpty())
        assertTrue(parseUsers("").isEmpty())
    }

    @Test
    fun `the screen offers only what the entry serves`() {
        // the live entry has reality and ws plus mtg, which is the Telegram flag
        // and never a client protocol
        assertEquals(listOf("reality", "ws"), userProtocols(listOf("reality", "ws", "mtg")))
        // an entry that serves hy2 must offer it, in the same fixed order
        assertEquals(listOf("reality", "hy2"), userProtocols(listOf("hy2", "reality")))
        assertEquals(listOf("hy2", "awg"), userProtocols(listOf("awg", "mtg", "hy2")))
        // Never invent inbounds that are not actually enabled.
        assertEquals(emptyList(), userProtocols(emptyList()))
        assertEquals(emptyList(), userProtocols(listOf("mtg", "unknown")))
    }

    @Test
    fun `a new user includes all subscription transports while AWG stays explicit`() {
        assertEquals(setOf("reality", "ws", "hy2"), defaultProtocols(listOf("reality", "ws", "hy2", "awg")))
        assertEquals(setOf("reality"), defaultProtocols(listOf("reality")))
        assertEquals(setOf("hy2"), defaultProtocols(listOf("hy2", "awg")))
        assertEquals(setOf("awg"), defaultProtocols(listOf("awg")))
        assertTrue(defaultProtocols(emptyList()).isEmpty())
    }

    @Test
    fun `the estate and its links parse as the core writes them`() {
        val hosts = parseHosts(
            """[{"name":"ru-msk-1","role":"entry","engine":"sing-box","domain":"gw.example.ru","via":""}]""",
        )

        assertEquals(1, hosts.size)
        assertEquals("entry", hosts[0].role)
        assertEquals("gw.example.ru", hosts[0].domain)

        val links = parseHostLinks("""[{"from":"ru-msk-1","to":"nl-ams-1","protocols":["hy2"],"active":"hy2"}]""")

        assertEquals(1, links.size)
        assertEquals("hy2", links[0].protocols)
        assertEquals("hy2", links[0].active)
    }

    @Test
    fun `devices parse with the recipient that marks this phone`() {
        val devices = parseDevices(
            """[{"name":"phone","recipient":"age1abc","added":"2026-11-01"},{"name":"laptop","recipient":"age1def"}]""",
        )

        assertEquals(2, devices.size)
        assertEquals("age1abc", devices[0].recipient)
        assertEquals("2026-11-01", devices[0].added)
        assertEquals("", devices[1].added)
    }
}
