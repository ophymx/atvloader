package com.ophymx.atvloader.ui

import android.app.Application
import android.os.Build
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import com.ophymx.atvloader.data.AppInfo
import com.ophymx.atvloader.data.DeviceCode
import com.ophymx.atvloader.data.Prefs
import com.ophymx.atvloader.data.ServerInfo
import com.ophymx.atvloader.data.StoreApi
import com.ophymx.atvloader.data.UnauthorizedException
import com.ophymx.atvloader.data.describeError
import com.ophymx.atvloader.install.InstallState
import com.ophymx.atvloader.install.Installer
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

sealed interface Screen {
    data object Setup : Screen
    data object Pairing : Screen
    data object Browse : Screen
    data class Detail(val pkg: String) : Screen
    data object Settings : Screen
}

enum class Status { NotInstalled, Installed, UpdateAvailable, Newer }

class StoreViewModel(app: Application) : AndroidViewModel(app) {
    val prefs = Prefs(app)
    val api = StoreApi(prefs)
    val selfPackage: String = app.packageName

    /** This build, for the settings screen: "0.1.0 (1000)". */
    val selfVersion: String = runCatching {
        val pi = app.packageManager.getPackageInfo(app.packageName, 0)
        val code = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) pi.longVersionCode else @Suppress("DEPRECATION") pi.versionCode.toLong()
        "${pi.versionName} ($code)"
    }.getOrDefault("unknown")
    val deviceModel: String = listOf(Build.MANUFACTURER, Build.MODEL).filter { it.isNotBlank() }.distinct().joinToString(" ")

    var screen by mutableStateOf<Screen>(if (prefs.serverUrl.isBlank()) Screen.Setup else if (prefs.deviceToken == null) Screen.Pairing else Screen.Browse)
        private set
    var serverInfo by mutableStateOf<ServerInfo?>(null)
    var apps by mutableStateOf<List<AppInfo>>(emptyList())
    var categories by mutableStateOf<List<String>>(emptyList())
    var installed by mutableStateOf<Map<String, Long>>(emptyMap())
    var loading by mutableStateOf(false)
    var error by mutableStateOf<String?>(null)
    var installStates by mutableStateOf<Map<String, InstallState>>(emptyMap())
    var needsUnknownSources by mutableStateOf(false)

    // Pairing
    var pairingCode by mutableStateOf<DeviceCode?>(null)
    var pairingError by mutableStateOf<String?>(null)
    private var pairingJob: Job? = null

    // Setup
    var setupError by mutableStateOf<String?>(null)
    var setupBusy by mutableStateOf(false)

    init {
        viewModelScope.launch { Installer.states.collect { installStates = it } }
        viewModelScope.launch { Installer.generation.collect { refreshInstalled() } }
        if (screen == Screen.Browse) refresh()
        if (screen == Screen.Pairing) startPairing()
    }

    fun refreshInstalled() {
        installed = Installer.installedVersions(getApplication())
    }

    fun statusOf(app: AppInfo): Status {
        val cur = installed[app.pkg] ?: return Status.NotInstalled
        val latest = app.latest?.versionCode ?: return Status.Installed
        return when {
            cur < latest -> Status.UpdateAvailable
            cur > latest -> Status.Newer
            else -> Status.Installed
        }
    }

    fun goTo(s: Screen) {
        screen = s
    }

    fun back(): Boolean = when (screen) {
        is Screen.Detail, Screen.Settings -> {
            screen = Screen.Browse; true
        }
        Screen.Pairing -> {
            stopPairing(); screen = Screen.Setup; true
        }
        else -> false
    }

    // ---- setup ---------------------------------------------------------------

    fun connect(url: String) {
        var u = url.trim()
        if (u.isEmpty()) {
            setupError = "Enter the address of your atvloader server"; return
        }
        if (!u.startsWith("http://") && !u.startsWith("https://")) u = "http://$u"
        setupBusy = true
        setupError = null
        viewModelScope.launch {
            try {
                val info = api.info(u)
                if (info.service != "atvloader") throw IllegalStateException("That address answered, but it is not an atvloader server")
                prefs.serverUrl = u
                serverInfo = info
                if (prefs.deviceToken != null) {
                    screen = Screen.Browse; refresh()
                } else {
                    screen = Screen.Pairing; startPairing()
                }
            } catch (e: Exception) {
                setupError = if (e is IllegalStateException) e.message else describeError(e, u)
            } finally {
                setupBusy = false
            }
        }
    }

    // ---- pairing -------------------------------------------------------------

    fun startPairing() {
        pairingJob?.cancel()
        pairingError = null
        pairingCode = null
        pairingJob = viewModelScope.launch {
            try {
                val code = api.requestDeviceCode()
                pairingCode = code
                val interval = code.interval.coerceAtLeast(2) * 1000L
                val deadline = System.currentTimeMillis() + code.expiresIn * 1000L
                while (System.currentTimeMillis() < deadline) {
                    delay(interval)
                    val t = api.pollDeviceToken(code.deviceCode) ?: continue
                    if (t.token != null) {
                        prefs.deviceToken = t.token
                        prefs.deviceName = t.deviceName ?: ""
                        screen = Screen.Browse
                        refresh()
                        return@launch
                    }
                    pairingError = "Pairing was ${t.status}"
                    return@launch
                }
                pairingError = "This code expired. Request a new one."
            } catch (e: Exception) {
                pairingError = "Could not start pairing. ${api.describe(e)}"
            }
        }
    }

    fun stopPairing() {
        pairingJob?.cancel(); pairingJob = null
    }

    fun unlink() {
        prefs.clearDevice()
        apps = emptyList()
        screen = Screen.Pairing
        startPairing()
    }

    fun changeServer() {
        stopPairing()
        screen = Screen.Setup
    }

    // ---- catalog -------------------------------------------------------------

    fun refresh() {
        loading = true
        error = null
        viewModelScope.launch {
            try {
                refreshInstalled()
                val r = api.apps()
                apps = r.apps
                categories = r.categories
                runCatching { serverInfo = api.info() }
            } catch (e: UnauthorizedException) {
                prefs.clearDevice()
                screen = Screen.Pairing
                startPairing()
            } catch (e: Exception) {
                error = api.describe(e)
            } finally {
                loading = false
            }
        }
    }

    fun app(pkg: String): AppInfo? = apps.firstOrNull { it.pkg == pkg }

    /** The store's copy of this very app, when it is newer than what is running. */
    val selfUpdate: AppInfo?
        get() = apps.firstOrNull { it.pkg == selfPackage }?.takeIf { statusOf(it) == Status.UpdateAvailable }

    // ---- install -------------------------------------------------------------

    fun install(app: AppInfo, versionCode: Long? = null) {
        val ctx = getApplication<Application>()
        if (!Installer.canInstall(ctx)) {
            needsUnknownSources = true
            return
        }
        val v = (if (versionCode == null) app.latest else app.versions.firstOrNull { it.versionCode == versionCode } ?: app.latest) ?: return
        viewModelScope.launch {
            Installer.install(ctx, api, app.pkg, v.downloadUrl, v.sha256)
        }
    }

    fun dismissUnknownSources() {
        needsUnknownSources = false
    }

    fun openUnknownSourcesSettings() {
        needsUnknownSources = false
        Installer.openUnknownSourcesSettings(getApplication())
    }

    fun clearInstallState(pkg: String) = Installer.clear(pkg)
    fun open(pkg: String) = Installer.open(getApplication(), pkg)
    fun uninstall(pkg: String) = Installer.uninstall(getApplication(), pkg)
    fun onResume() {
        Installer.bumpGeneration()
    }
}
