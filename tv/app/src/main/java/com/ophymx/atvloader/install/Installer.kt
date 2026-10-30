package com.ophymx.atvloader.install

import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInstaller
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.provider.Settings
import android.util.Log
import com.ophymx.atvloader.data.StoreApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.update
import java.io.File

sealed interface InstallState {
    data object Idle : InstallState
    data class Downloading(val done: Long, val total: Long) : InstallState
    data object Installing : InstallState
    data object AwaitingConfirm : InstallState
    data class Failed(val message: String) : InstallState
    data object Done : InstallState
}

/**
 * Downloads an APK through the store API and hands it to PackageInstaller.
 * Process-wide singleton so the broadcast receiver can report back.
 */
object Installer {
    private const val TAG = "Installer"
    const val ACTION_STATUS = "com.ophymx.atvloader.INSTALL_STATUS"
    const val EXTRA_PACKAGE = "atvloader.package"

    private val _states = MutableStateFlow<Map<String, InstallState>>(emptyMap())
    val states: StateFlow<Map<String, InstallState>> = _states

    /** Bumped whenever the set of installed packages may have changed. */
    private val _generation = MutableStateFlow(0)
    val generation: StateFlow<Int> = _generation

    fun set(pkg: String, s: InstallState) = _states.update { it + (pkg to s) }
    fun clear(pkg: String) = _states.update { it - pkg }

    fun canInstall(context: Context): Boolean =
        Build.VERSION.SDK_INT < Build.VERSION_CODES.O || context.packageManager.canRequestPackageInstalls()

    /** Opens the right settings page for "install unknown apps"; TVs vary, so try a few. */
    fun openUnknownSourcesSettings(context: Context) {
        val candidates = listOf(
            Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:${context.packageName}")),
            Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES),
            Intent(Settings.ACTION_SECURITY_SETTINGS),
            Intent(Settings.ACTION_SETTINGS),
        )
        for (i in candidates) {
            try {
                context.startActivity(i.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                return
            } catch (_: Exception) {
            }
        }
    }

    suspend fun install(context: Context, api: StoreApi, pkg: String, downloadUrl: String, sha256: String = "") {
        val app = context.applicationContext
        val file = File(app.cacheDir, "downloads/$pkg.apk")
        try {
            set(pkg, InstallState.Downloading(0, -1))
            api.download(downloadUrl, file, sha256) { done, total -> set(pkg, InstallState.Downloading(done, total)) }
            set(pkg, InstallState.Installing)
            commit(app, pkg, file)
        } catch (e: Exception) {
            Log.w(TAG, "install $pkg failed", e)
            file.delete()
            set(pkg, InstallState.Failed(e.message ?: e.javaClass.simpleName))
        }
    }

    private fun commit(context: Context, pkg: String, file: File) {
        val installer = context.packageManager.packageInstaller
        val params = PackageInstaller.SessionParams(PackageInstaller.SessionParams.MODE_FULL_INSTALL).apply {
            setAppPackageName(pkg)
            setSize(file.length())
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) setRequireUserAction(PackageInstaller.SessionParams.USER_ACTION_NOT_REQUIRED)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) setPackageSource(PackageInstaller.PACKAGE_SOURCE_STORE)
        }
        val sessionId = installer.createSession(params)
        installer.openSession(sessionId).use { session ->
            session.openWrite("base.apk", 0, file.length()).use { out ->
                file.inputStream().use { it.copyTo(out) }
                session.fsync(out)
            }
            val intent = Intent(context, InstallReceiver::class.java)
                .setAction(ACTION_STATUS)
                .putExtra(EXTRA_PACKAGE, pkg)
            var flags = PendingIntent.FLAG_UPDATE_CURRENT
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) flags = flags or PendingIntent.FLAG_MUTABLE
            val pi = PendingIntent.getBroadcast(context, sessionId, intent, flags)
            session.commit(pi.intentSender)
        }
        file.delete()
    }

    /** Called by [InstallReceiver] with the installer's status broadcast. */
    fun onStatus(context: Context, intent: Intent) {
        val pkg = intent.getStringExtra(EXTRA_PACKAGE) ?: intent.getStringExtra(PackageInstaller.EXTRA_PACKAGE_NAME) ?: return
        when (val status = intent.getIntExtra(PackageInstaller.EXTRA_STATUS, PackageInstaller.STATUS_FAILURE)) {
            PackageInstaller.STATUS_PENDING_USER_ACTION -> {
                set(pkg, InstallState.AwaitingConfirm)
                @Suppress("DEPRECATION")
                val confirm = intent.getParcelableExtra<Intent>(Intent.EXTRA_INTENT)
                if (confirm != null) {
                    try {
                        context.startActivity(confirm.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                    } catch (e: Exception) {
                        set(pkg, InstallState.Failed("could not show the install confirmation: ${e.message}"))
                    }
                } else {
                    set(pkg, InstallState.Failed("installer asked for confirmation but sent no intent"))
                }
            }
            PackageInstaller.STATUS_SUCCESS -> {
                set(pkg, InstallState.Done)
                _generation.update { it + 1 }
            }
            else -> {
                val msg = intent.getStringExtra(PackageInstaller.EXTRA_STATUS_MESSAGE) ?: "installer status $status"
                set(pkg, InstallState.Failed(friendly(status, msg)))
                _generation.update { it + 1 }
            }
        }
    }

    private fun friendly(status: Int, raw: String): String = when (status) {
        PackageInstaller.STATUS_FAILURE_ABORTED -> "Install cancelled"
        PackageInstaller.STATUS_FAILURE_BLOCKED -> "Install blocked by the system ($raw)"
        PackageInstaller.STATUS_FAILURE_CONFLICT -> "Conflicts with the installed version. Uninstall it first, or the signing key differs. ($raw)"
        PackageInstaller.STATUS_FAILURE_INCOMPATIBLE -> "This APK is not compatible with this device ($raw)"
        PackageInstaller.STATUS_FAILURE_INVALID -> "The APK is invalid ($raw)"
        PackageInstaller.STATUS_FAILURE_STORAGE -> "Not enough storage ($raw)"
        else -> raw
    }

    fun uninstall(context: Context, pkg: String) {
        @Suppress("DEPRECATION")
        val i = Intent(Intent.ACTION_UNINSTALL_PACKAGE, Uri.parse("package:$pkg")).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        try {
            context.startActivity(i)
        } catch (_: Exception) {
            context.startActivity(Intent(Intent.ACTION_DELETE, Uri.parse("package:$pkg")).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        }
    }

    fun open(context: Context, pkg: String): Boolean {
        val pm = context.packageManager
        val i = pm.getLeanbackLaunchIntentForPackage(pkg) ?: pm.getLaunchIntentForPackage(pkg) ?: return false
        context.startActivity(i.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        return true
    }

    /** Installed versionCode for every package, so the UI can show installed/update state. */
    fun installedVersions(context: Context): Map<String, Long> {
        val pm = context.packageManager
        @Suppress("DEPRECATION")
        val list = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU)
            pm.getInstalledPackages(PackageManager.PackageInfoFlags.of(0)) else pm.getInstalledPackages(0)
        return list.associate { p ->
            @Suppress("DEPRECATION")
            val code = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) p.longVersionCode else p.versionCode.toLong()
            p.packageName to code
        }
    }

    fun bumpGeneration() = _generation.update { it + 1 }
}
