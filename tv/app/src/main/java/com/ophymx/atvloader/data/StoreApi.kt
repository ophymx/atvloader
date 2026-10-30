package com.ophymx.atvloader.data

import android.os.Build
import com.google.gson.Gson
import com.google.gson.JsonObject
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import java.io.File
import java.io.IOException
import java.util.concurrent.TimeUnit

class ApiException(val code: Int, message: String) : IOException(message)
class UnauthorizedException : IOException("device token rejected")

/** HTTP client for the atvloader server. One instance per app; base URL and token come from Prefs. */
class StoreApi(private val prefs: Prefs) {
    private val gson = Gson()
    private val json = "application/json".toMediaType()

    val client: OkHttpClient = OkHttpClient.Builder()
        .connectTimeout(10, TimeUnit.SECONDS)
        .readTimeout(60, TimeUnit.SECONDS)
        .addInterceptor { chain ->
            val token = prefs.deviceToken
            val req = if (token != null) chain.request().newBuilder().header("Authorization", "Bearer $token").build() else chain.request()
            chain.proceed(req)
        }
        .build()

    fun url(path: String): String = prefs.serverUrl.trimEnd('/') + path

    /** A sentence for the screen about [e], naming this server. */
    fun describe(e: Throwable): String = describeError(e, prefs.serverUrl)

    private inline fun <reified T> get(path: String): T = run(Request.Builder().url(url(path)).get().build())

    private inline fun <reified T> post(path: String, body: Any): T =
        run(Request.Builder().url(url(path)).post(gson.toJson(body).toRequestBody(json)).build())

    private inline fun <reified T> run(req: Request): T {
        client.newCall(req).execute().use { resp ->
            val text = resp.body.string()
            if (resp.code == 401) throw UnauthorizedException()
            if (!resp.isSuccessful) {
                val msg = runCatching { gson.fromJson(text, ApiError::class.java).error }.getOrNull()
                throw ApiException(resp.code, msg?.takeIf { it.isNotBlank() } ?: "HTTP ${resp.code}")
            }
            return gson.fromJson(text, T::class.java)
        }
    }

    suspend fun info(baseUrl: String? = null): ServerInfo = withContext(Dispatchers.IO) {
        val base = (baseUrl ?: prefs.serverUrl).trimEnd('/')
        run(Request.Builder().url("$base/api/v1/info").get().build())
    }

    suspend fun requestDeviceCode(): DeviceCode = withContext(Dispatchers.IO) {
        val body = JsonObject().apply {
            addProperty("model", listOfNotNull(Build.MANUFACTURER, Build.MODEL).joinToString(" "))
            addProperty("product", Build.PRODUCT ?: "")
            addProperty("sdk", Build.VERSION.SDK_INT)
        }
        post("/api/v1/device/code", body)
    }

    /** Returns null while the code is still pending. Throws ApiException once it expires. */
    suspend fun pollDeviceToken(deviceCode: String): DeviceToken? = withContext(Dispatchers.IO) {
        val body = JsonObject().apply { addProperty("deviceCode", deviceCode) }
        val t: DeviceToken = post("/api/v1/device/token", body)
        if (t.status == "pending") null else t
    }

    suspend fun apps(): AppsResponse = withContext(Dispatchers.IO) { get("/api/v1/apps") }

    suspend fun app(pkg: String): AppInfo = withContext(Dispatchers.IO) { get("/api/v1/apps/$pkg") }

    /**
     * Streams an APK to [dest], reporting (bytesSoFar, total) as it goes. total is -1 when unknown.
     *
     * The file is verified against [expectedSha256] (the catalog's hash for this version; when the caller
     * has none, the X-SHA256 header the server sends with the file is used). See [copyVerified].
     */
    suspend fun download(downloadUrl: String, dest: File, expectedSha256: String = "", onProgress: (Long, Long) -> Unit) = withContext(Dispatchers.IO) {
        val req = Request.Builder().url(url(downloadUrl)).get().build()
        client.newCall(req).execute().use { resp ->
            if (resp.code == 401) throw UnauthorizedException()
            if (!resp.isSuccessful) throw ApiException(resp.code, "download failed: HTTP ${resp.code}")
            val body = resp.body
            val expected = expectedSha256.ifBlank { resp.header("X-SHA256").orEmpty() }
            body.byteStream().use { input -> copyVerified(input, dest, body.contentLength(), expected, onProgress) }
        }
    }
}
