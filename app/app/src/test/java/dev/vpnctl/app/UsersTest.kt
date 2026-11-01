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
        // nothing readable at all still leaves the command line's default
        assertEquals(listOf("reality", "ws"), userProtocols(emptyList()))
    }

    @Test
    fun `a new user starts with the pair the command line picks`() {
        assertEquals(setOf("reality", "ws"), defaultProtocols(listOf("reality", "ws", "hy2")))
        assertEquals(setOf("reality"), defaultProtocols(listOf("reality")))
    }
}
