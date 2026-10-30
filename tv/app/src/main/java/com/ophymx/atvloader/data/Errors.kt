package com.ophymx.atvloader.data

import java.net.ConnectException
import java.net.SocketTimeoutException
import java.net.UnknownHostException
import javax.net.ssl.SSLException

/**
 * Turns an exception from a store call into a sentence for the screen. OkHttp's own messages
 * ("Failed to connect to /192.168.1.10:8080") are accurate but read like a stack trace on a TV.
 */
fun describeError(e: Throwable, serverUrl: String): String {
    val where = serverUrl.ifBlank { "the server" }
    return when (e) {
        is UnauthorizedException -> "This TV is no longer linked to the store."
        is IntegrityException -> e.message ?: "The download did not match what the store promised."
        is ApiException -> "The store answered HTTP ${e.code}: ${e.message}"
        is UnknownHostException -> "Can't find $where. Check the address and the TV's network."
        is ConnectException -> "Nothing answered at $where. Is the server running?"
        is SocketTimeoutException -> "$where did not answer in time."
        is SSLException -> "Secure connection to $where failed: ${e.message ?: "certificate problem"}"
        else -> e.message?.takeIf { it.isNotBlank() } ?: e.javaClass.simpleName
    }
}
