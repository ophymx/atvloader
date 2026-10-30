package com.ophymx.atvloader.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.security.MessageDigest

class VerifiedCopyTest {
    @get:Rule val tmp = TemporaryFolder()

    private val payload = ByteArray(1_300_000) { (it * 31 % 251).toByte() }
    private val payloadHash = MessageDigest.getInstance("SHA-256").digest(payload).joinToString("") { "%02x".format(it) }

    @Test fun acceptsMatchingHashAndLength() {
        val dest = tmp.newFile()
        val reports = mutableListOf<Pair<Long, Long>>()
        val n = copyVerified(payload.inputStream(), dest, payload.size.toLong(), payloadHash) { d, t -> reports += d to t }
        assertEquals(payload.size.toLong(), n)
        assertTrue(dest.readBytes().contentEquals(payload))
        assertEquals(payload.size.toLong(), reports.last().first)
        assertTrue("progress should be reported along the way", reports.size >= 2)
    }

    @Test fun hashIsCaseInsensitiveAndUnknownLengthIsFine() {
        val dest = tmp.newFile()
        copyVerified(payload.inputStream(), dest, -1, payloadHash.uppercase()) { _, _ -> }
        assertTrue(dest.exists())
    }

    @Test fun blankHashSkipsOnlyTheHashCheck() {
        val dest = tmp.newFile()
        copyVerified(payload.inputStream(), dest, payload.size.toLong(), "") { _, _ -> }
        assertTrue(dest.exists())
        try {
            copyVerified(payload.inputStream(), dest, payload.size + 1L, "") { _, _ -> }
            fail("short read must be rejected even without a hash")
        } catch (e: IntegrityException) {
            assertTrue(e.message!!.contains("incomplete"))
            assertFalse(dest.exists())
        }
    }

    @Test fun rejectsAndDeletesOnHashMismatch() {
        val dest = tmp.newFile()
        val tampered = payload.copyOf().also { it[12345] = (it[12345] + 1).toByte() }
        try {
            copyVerified(tampered.inputStream(), dest, tampered.size.toLong(), payloadHash) { _, _ -> }
            fail("tampered content must be rejected")
        } catch (e: IntegrityException) {
            assertTrue(e.message!!.contains("checksum"))
            assertFalse(dest.exists())
        }
    }

    @Test fun rejectsAndDeletesOnShortRead() {
        val dest = tmp.newFile()
        try {
            copyVerified(payload.inputStream(0, 1000), dest, payload.size.toLong(), payloadHash) { _, _ -> }
            fail("truncated content must be rejected")
        } catch (e: IntegrityException) {
            assertTrue(e.message!!.contains("incomplete"))
            assertFalse(dest.exists())
        }
    }
}
