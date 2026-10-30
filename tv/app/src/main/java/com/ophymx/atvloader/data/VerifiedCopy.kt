package com.ophymx.atvloader.data

import java.io.File
import java.io.IOException
import java.io.InputStream
import java.security.MessageDigest

/** A download that did not match what the catalog promised; the file has already been removed. */
class IntegrityException(message: String) : IOException(message)

/**
 * Copies [input] to [dest] while hashing it, then checks the result against what the store said.
 *
 * [total] is the announced length (-1 when unknown); [expectedSha256] is the catalog hash, lowercase
 * or uppercase hex, or blank to skip the hash check. On a short read or a mismatch the file is deleted
 * and [IntegrityException] is thrown, so a corrupted or substituted APK never reaches the installer.
 * Returns the number of bytes written.
 */
fun copyVerified(input: InputStream, dest: File, total: Long, expectedSha256: String, onProgress: (Long, Long) -> Unit): Long {
    val expected = expectedSha256.trim().lowercase()
    val digest = MessageDigest.getInstance("SHA-256")
    dest.parentFile?.mkdirs()
    var done = 0L
    dest.outputStream().use { out ->
        val buf = ByteArray(256 * 1024)
        var lastReport = 0L
        while (true) {
            val n = input.read(buf)
            if (n < 0) break
            out.write(buf, 0, n)
            digest.update(buf, 0, n)
            done += n
            if (done - lastReport > 512 * 1024 || done == total) {
                lastReport = done
                onProgress(done, total)
            }
        }
        onProgress(done, total)
    }
    if (total >= 0 && done != total) {
        dest.delete()
        throw IntegrityException("download incomplete: got $done of $total bytes")
    }
    val actual = digest.digest().joinToString("") { "%02x".format(it) }
    if (expected.isNotEmpty() && actual != expected) {
        dest.delete()
        throw IntegrityException("download corrupted: checksum does not match the store")
    }
    return done
}
