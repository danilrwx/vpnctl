package dev.vpnctl.app

import kotlin.test.Test
import kotlin.test.assertEquals

/** The repository field takes what a person pastes, not only git@host:path. */
class RepoTest {
    @Test
    fun `all the github spellings become the ssh remote`() {
        val want = "git@github.com:danilrwx/vpn.git"

        assertEquals(want, normalizeRepo("danilrwx/vpn"))
        assertEquals(want, normalizeRepo("github.com/danilrwx/vpn"))
        assertEquals(want, normalizeRepo("https://github.com/danilrwx/vpn"))
        assertEquals(want, normalizeRepo("https://github.com/danilrwx/vpn/"))
        assertEquals(want, normalizeRepo("https://github.com/danilrwx/vpn.git"))
        assertEquals(want, normalizeRepo("  danilrwx/vpn  "))
    }

    @Test
    fun `an ssh remote is left alone`() {
        val ssh = "git@github.com:danilrwx/vpn.git"

        assertEquals(ssh, normalizeRepo(ssh))
        assertEquals("ssh://git@github.com/danilrwx/vpn.git", normalizeRepo("ssh://git@github.com/danilrwx/vpn.git"))
    }

    @Test
    fun `an empty field stays empty`() {
        assertEquals("", normalizeRepo(""))
        assertEquals("", normalizeRepo("   "))
    }
}
