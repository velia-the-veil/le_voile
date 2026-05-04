package fr.plateformeliberte.levoile.update

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Story 12.5 — tests SemVer 2.0.0 (parse + compareTo).
 *
 * Spec : https://semver.org/spec/v2.0.0.html
 */
class SemVerCompareTest {

    // ---------- Parse ----------

    @Test fun `parse 0-1-0`() {
        assertEquals(SemVer(0, 1, 0), SemVer.parse("0.1.0"))
    }

    @Test fun `parse v0-1-0 prefix`() {
        assertEquals(SemVer(0, 1, 0), SemVer.parse("v0.1.0"))
    }

    @Test fun `parse pre-release`() {
        assertEquals(SemVer(0, 1, 0, "beta.1"), SemVer.parse("0.1.0-beta.1"))
    }

    @Test fun `parse build metadata`() {
        assertEquals(SemVer(0, 1, 0, null, "20260503"), SemVer.parse("0.1.0+20260503"))
    }

    @Test fun `parse pre-release et build metadata`() {
        assertEquals(
            SemVer(0, 1, 0, "rc.1", "abc123"),
            SemVer.parse("0.1.0-rc.1+abc123"),
        )
    }

    @Test fun `parse invalid returns null`() {
        assertNull(SemVer.parse("invalid"))
        assertNull(SemVer.parse("1.2"))
        assertNull(SemVer.parse("1.2.3.4"))
        assertNull(SemVer.parse(""))
    }

    // ---------- compareTo : major / minor / patch ----------

    @Test fun `compare major`() {
        assertTrue(SemVer.parse("2.0.0")!! > SemVer.parse("1.99.99")!!)
    }

    @Test fun `compare minor`() {
        assertTrue(SemVer.parse("1.2.0")!! > SemVer.parse("1.1.99")!!)
    }

    @Test fun `compare patch`() {
        assertTrue(SemVer.parse("1.0.2")!! > SemVer.parse("1.0.1")!!)
    }

    // ---------- compareTo : pre-release ranking (SemVer §11.4) ----------

    @Test fun `pre-release inferieur a release`() {
        assertTrue(SemVer.parse("1.0.0")!! > SemVer.parse("1.0.0-beta.1")!!)
    }

    @Test fun `compare pre-release numeric ascending`() {
        assertTrue(SemVer.parse("1.0.0-beta.2")!! > SemVer.parse("1.0.0-beta.1")!!)
    }

    @Test fun `compare pre-release rc beats beta`() {
        assertTrue(SemVer.parse("1.0.0-rc.1")!! > SemVer.parse("1.0.0-beta.1")!!)
    }

    @Test fun `compare pre-release alpha inferieur a beta`() {
        assertTrue(SemVer.parse("1.0.0-beta.1")!! > SemVer.parse("1.0.0-alpha.1")!!)
    }

    @Test fun `compare pre-release numeric inferieur a alphanum (SemVer 11-4-3)`() {
        // 1.0.0-1 < 1.0.0-rc1 (numeric < alpha)
        assertTrue(SemVer.parse("1.0.0-rc1")!! > SemVer.parse("1.0.0-1")!!)
    }

    // ---------- compareTo : build metadata ignoré ----------

    @Test fun `build metadata ignored in compare`() {
        assertEquals(
            0,
            SemVer.parse("1.0.0+a")!!.compareTo(SemVer.parse("1.0.0+b")!!),
        )
    }

    @Test fun `equal versions`() {
        assertEquals(0, SemVer.parse("1.0.0")!!.compareTo(SemVer.parse("1.0.0")!!))
    }

    @Test fun `tag GitHub typique v0-1-0 versus v0-2-0`() {
        // Use case Story 12.5 : tag GitHub `v0.2.0` doit être détecté comme > `v0.1.0`.
        assertTrue(SemVer.parse("v0.2.0")!! > SemVer.parse("v0.1.0")!!)
    }
}
