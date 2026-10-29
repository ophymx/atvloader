@file:OptIn(ExperimentalTvMaterial3Api::class, ExperimentalFoundationApi::class)

package com.ophymx.atvloader.ui

import androidx.tv.material3.ExperimentalTvMaterial3Api
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.focusRestorer
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.tv.material3.Button
import androidx.tv.material3.Card
import androidx.tv.material3.CardDefaults
import androidx.tv.material3.MaterialTheme
import androidx.tv.material3.OutlinedButton
import androidx.tv.material3.Text
import coil3.ImageLoader
import coil3.compose.AsyncImage
import com.ophymx.atvloader.data.AppInfo
import com.ophymx.atvloader.install.InstallState

private val Gutter = 48.dp

// ---------------------------------------------------------------- Setup

@Composable
fun SetupScreen(vm: StoreViewModel) {
    var url by rememberSaveable { mutableStateOf(vm.prefs.serverUrl) }
    val fr = remember { FocusRequester() }
    LaunchedEffect(Unit) { fr.requestFocus() }
    Column(Modifier.fillMaxSize().padding(Gutter), verticalArrangement = Arrangement.Center) {
        Text("ATV Loader", style = MaterialTheme.typography.displaySmall, color = Fog, fontWeight = FontWeight.Bold)
        Spacer(Modifier.height(8.dp))
        Text("Where is your app store?", color = Mist, fontSize = 20.sp)
        Spacer(Modifier.height(32.dp))
        TvTextField(
            value = url, onValueChange = { url = it; vm.setupError = null },
            placeholder = "http://192.168.1.10:8080", keyboardType = KeyboardType.Uri,
            onDone = { vm.connect(url) },
            modifier = Modifier.widthIn(max = 640.dp).focusRequester(fr),
        )
        Spacer(Modifier.height(16.dp))
        Button(onClick = { vm.connect(url) }, enabled = !vm.setupBusy) {
            Text(if (vm.setupBusy) "Connecting…" else "Connect")
        }
        vm.setupError?.let {
            Spacer(Modifier.height(16.dp))
            Text(it, color = Coral, fontSize = 16.sp)
        }
    }
}

// ---------------------------------------------------------------- Pairing

@Composable
fun PairingScreen(vm: StoreViewModel) {
    val fr = remember { FocusRequester() }
    LaunchedEffect(vm.pairingCode, vm.pairingError) { runCatching { fr.requestFocus() } }
    Column(Modifier.fillMaxSize().padding(Gutter), verticalArrangement = Arrangement.Center, horizontalAlignment = Alignment.CenterHorizontally) {
        Text("Link this TV", style = MaterialTheme.typography.headlineMedium, color = Fog)
        Spacer(Modifier.height(8.dp))
        Text("Open the admin page on another device and enter this code under Devices.", color = Mist, fontSize = 18.sp)
        Spacer(Modifier.height(40.dp))
        val code = vm.pairingCode
        when {
            vm.pairingError != null -> {
                Text(vm.pairingError!!, color = Coral, fontSize = 18.sp)
                Spacer(Modifier.height(24.dp))
                Row {
                    Button(onClick = { vm.startPairing() }, modifier = Modifier.focusRequester(fr)) { Text("Get a new code") }
                    Spacer(Modifier.width(12.dp))
                    OutlinedButton(onClick = { vm.changeServer() }) { Text("Change server") }
                }
            }
            code == null -> Text("Requesting a code…", color = Mist, fontSize = 18.sp)
            else -> {
                Box(Modifier.clip(RoundedCornerShape(16.dp)).background(Panel).padding(horizontal = 48.dp, vertical = 24.dp)) {
                    Text(code.userCode, fontSize = 72.sp, fontWeight = FontWeight.Bold, color = Lime, letterSpacing = 8.sp)
                }
                Spacer(Modifier.height(16.dp))
                Text(vm.serverInfo?.let { "Store: ${it.name}" } ?: vm.prefs.serverUrl, color = Mist, fontSize = 16.sp)
                Spacer(Modifier.height(32.dp))
                OutlinedButton(onClick = { vm.changeServer() }, modifier = Modifier.focusRequester(fr)) { Text("Change server") }
            }
        }
    }
}

// ---------------------------------------------------------------- Browse

private data class Shelf(val title: String, val apps: List<AppInfo>)

@Composable
fun BrowseScreen(vm: StoreViewModel, imageLoader: ImageLoader) {
    val shelves = remember(vm.apps, vm.categories, vm.installed) { buildShelves(vm) }
    val firstFocus = remember { FocusRequester() }
    LaunchedEffect(shelves.isNotEmpty()) { if (shelves.isNotEmpty()) runCatching { firstFocus.requestFocus() } }

    Column(Modifier.fillMaxSize()) {
        Row(
            Modifier.fillMaxWidth().padding(start = Gutter, end = Gutter, top = 28.dp, bottom = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f)) {
                Text(vm.serverInfo?.name?.ifBlank { null } ?: "ATV Loader", style = MaterialTheme.typography.headlineMedium, color = Fog)
                Text(
                    if (vm.loading) "Loading…" else "${vm.apps.size} apps",
                    color = Mist, fontSize = 14.sp,
                )
            }
            OutlinedButton(onClick = { vm.refresh() }) { Text("Refresh") }
            Spacer(Modifier.width(8.dp))
            OutlinedButton(onClick = { vm.goTo(Screen.Settings) }) { Text("Settings") }
        }

        vm.error?.let {
            Text(it, color = Coral, modifier = Modifier.padding(horizontal = Gutter, vertical = 8.dp))
        }

        vm.selfUpdate?.let { self -> SelfUpdateBanner(vm, self) }

        if (shelves.isEmpty() && !vm.loading) {
            Column(Modifier.fillMaxSize().padding(Gutter), verticalArrangement = Arrangement.Center, horizontalAlignment = Alignment.CenterHorizontally) {
                Text("Nothing here yet", style = MaterialTheme.typography.headlineSmall, color = Fog)
                Spacer(Modifier.height(8.dp))
                Text("Upload an APK from the admin page and it will show up here.", color = Mist)
            }
            return
        }

        LazyColumn(contentPadding = PaddingValues(bottom = 40.dp), verticalArrangement = Arrangement.spacedBy(20.dp)) {
            items(shelves.size) { i ->
                val shelf = shelves[i]
                Column {
                    Text(shelf.title, color = Fog, fontSize = 20.sp, fontWeight = FontWeight.SemiBold, modifier = Modifier.padding(horizontal = Gutter, vertical = 8.dp))
                    LazyRow(
                        contentPadding = PaddingValues(horizontal = Gutter),
                        horizontalArrangement = Arrangement.spacedBy(16.dp),
                        modifier = Modifier.focusRestorer(),
                    ) {
                        items(shelf.apps, key = { it.pkg }) { app ->
                            val mod = if (i == 0 && app == shelf.apps.first()) Modifier.focusRequester(firstFocus) else Modifier
                            AppCard(app, vm.statusOf(app), vm.installStates[app.pkg], vm.api.url(app.iconUrl ?: ""), imageLoader, mod) {
                                vm.goTo(Screen.Detail(app.pkg))
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun SelfUpdateBanner(vm: StoreViewModel, self: AppInfo) {
    val install = vm.installStates[self.pkg]
    val busy = install is InstallState.Downloading || install is InstallState.Installing || install is InstallState.AwaitingConfirm
    Row(
        Modifier.padding(horizontal = Gutter, vertical = 8.dp).fillMaxWidth()
            .clip(RoundedCornerShape(12.dp)).background(PanelRaised).padding(horizontal = 20.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text("ATV Loader ${self.latest?.versionName ?: ""} is available", color = Fog, fontSize = 17.sp, fontWeight = FontWeight.Medium)
            Text(
                when (install) {
                    is InstallState.Downloading -> "Downloading… ${humanSize(install.done)}"
                    is InstallState.Installing, InstallState.AwaitingConfirm -> "Installing. The app restarts when it is done."
                    is InstallState.Failed -> install.message
                    else -> self.latest?.changelog?.ifBlank { null } ?: "Update this app to get the latest fixes and features."
                },
                color = if (install is InstallState.Failed) Coral else Mist, fontSize = 14.sp,
            )
        }
        Spacer(Modifier.width(16.dp))
        Button(onClick = { vm.clearInstallState(self.pkg); vm.install(self) }, enabled = !busy) { Text("Update now") }
    }
}

private fun buildShelves(vm: StoreViewModel): List<Shelf> {
    val apps = vm.apps
    if (apps.isEmpty()) return emptyList()
    val out = mutableListOf<Shelf>()
    val updates = apps.filter { vm.statusOf(it) == Status.UpdateAvailable }
    if (updates.isNotEmpty()) out += Shelf("Updates available", updates)
    val featured = apps.filter { it.featured }
    if (featured.isNotEmpty()) out += Shelf("Featured", featured)
    val byCat = apps.groupBy { it.category.ifBlank { "" } }
    val ordered = vm.categories + byCat.keys.filter { it.isNotBlank() && it !in vm.categories }.sorted()
    for (c in ordered.distinct()) byCat[c]?.let { out += Shelf(c, it) }
    byCat[""]?.let { out += Shelf(if (out.isEmpty()) "All apps" else "More apps", it) }
    val installedApps = apps.filter { vm.statusOf(it) != Status.NotInstalled }
    if (installedApps.isNotEmpty()) out += Shelf("Installed", installedApps)
    return out
}

@Composable
private fun AppCard(
    app: AppInfo, status: Status, install: InstallState?, iconUrl: String, imageLoader: ImageLoader,
    modifier: Modifier = Modifier, onClick: () -> Unit,
) {
    Card(
        onClick = onClick,
        modifier = modifier.width(200.dp),
        colors = CardDefaults.colors(containerColor = Panel, focusedContainerColor = PanelRaised),
        shape = CardDefaults.shape(RoundedCornerShape(12.dp)),
    ) {
        Column(Modifier.padding(16.dp)) {
            AppIcon(iconUrl, app.name, 72.dp, imageLoader)
            Spacer(Modifier.height(12.dp))
            Text(app.name, color = Fog, fontSize = 16.sp, fontWeight = FontWeight.Medium, maxLines = 1)
            Text(app.latest?.versionName ?: "", color = Mist, fontSize = 12.sp, maxLines = 1)
            Spacer(Modifier.height(8.dp))
            when {
                install is InstallState.Downloading -> ProgressBar(if (install.total > 0) install.done.toFloat() / install.total else null)
                install is InstallState.Installing || install is InstallState.AwaitingConfirm -> StatusPill("Installing", Amber)
                install is InstallState.Failed -> StatusPill("Failed", Coral)
                status == Status.Installed -> StatusPill("Installed", Mist)
                status == Status.UpdateAvailable -> StatusPill("Update", Amber)
                status == Status.Newer -> StatusPill("Installed", Mist)
                else -> Spacer(Modifier.height(20.dp))
            }
        }
    }
}

@Composable
private fun AppIcon(url: String, name: String, size: androidx.compose.ui.unit.Dp, imageLoader: ImageLoader) {
    Box(Modifier.size(size).clip(RoundedCornerShape(size / 5)).background(PanelRaised), contentAlignment = Alignment.Center) {
        if (url.endsWith("/icon") || url.contains("/icon?")) {
            AsyncImage(model = url, contentDescription = null, imageLoader = imageLoader, modifier = Modifier.fillMaxSize())
        } else {
            Text(name.take(1).uppercase(), color = Mist, fontSize = (size.value / 2.2).sp, fontWeight = FontWeight.Bold)
        }
    }
}

// ---------------------------------------------------------------- Detail

@Composable
fun DetailScreen(vm: StoreViewModel, pkg: String, imageLoader: ImageLoader) {
    val app = vm.app(pkg)
    if (app == null) {
        Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { Text("App not found", color = Mist) }
        return
    }
    val status = vm.statusOf(app)
    val install = vm.installStates[pkg]
    val fr = remember { FocusRequester() }
    LaunchedEffect(Unit) { runCatching { fr.requestFocus() } }
    val busy = install is InstallState.Downloading || install is InstallState.Installing || install is InstallState.AwaitingConfirm

    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(Gutter)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            AppIcon(vm.api.url(app.iconUrl ?: ""), app.name, 128.dp, imageLoader)
            Spacer(Modifier.width(32.dp))
            Column(Modifier.weight(1f)) {
                Text(app.name, style = MaterialTheme.typography.headlineLarge, color = Fog)
                Text(app.pkg, color = Mist, fontSize = 14.sp)
                Spacer(Modifier.height(8.dp))
                val latest = app.latest
                val installedCode = vm.installed[pkg]
                val line = buildString {
                    if (latest != null) append("Version ${latest.versionName} (${latest.versionCode}) · ${humanSize(latest.size)}")
                    if (installedCode != null) {
                        append(" · ")
                        append(
                            when (status) {
                                Status.Installed -> "Installed"
                                Status.UpdateAvailable -> "Installed version $installedCode, update available"
                                Status.Newer -> "Installed version $installedCode is newer"
                                else -> ""
                            }
                        )
                    }
                }
                Text(line, color = Mist, fontSize = 16.sp)
                if (app.category.isNotBlank()) Text(app.category, color = Mist, fontSize = 14.sp)
            }
        }
        Spacer(Modifier.height(28.dp))

        Row(verticalAlignment = Alignment.CenterVertically) {
            val primaryLabel = when {
                install is InstallState.Downloading -> "Downloading…"
                install is InstallState.Installing -> "Installing…"
                install is InstallState.AwaitingConfirm -> "Confirm on screen…"
                status == Status.UpdateAvailable -> "Update"
                status == Status.Installed || status == Status.Newer -> "Reinstall"
                else -> "Install"
            }
            Button(onClick = { vm.clearInstallState(pkg); vm.install(app) }, enabled = !busy && app.latest != null, modifier = Modifier.focusRequester(fr)) {
                Text(primaryLabel)
            }
            if (status != Status.NotInstalled) {
                Spacer(Modifier.width(12.dp))
                OutlinedButton(onClick = { vm.open(pkg) }) { Text("Open") }
                Spacer(Modifier.width(12.dp))
                OutlinedButton(onClick = { vm.uninstall(pkg) }) { Text("Uninstall") }
            }
        }

        when (install) {
            is InstallState.Downloading -> {
                Spacer(Modifier.height(16.dp))
                ProgressBar(if (install.total > 0) install.done.toFloat() / install.total else null, Modifier.widthIn(max = 480.dp).fillMaxWidth())
                Text("${humanSize(install.done)}${if (install.total > 0) " of ${humanSize(install.total)}" else ""}", color = Mist, fontSize = 14.sp)
            }
            is InstallState.Failed -> {
                Spacer(Modifier.height(16.dp))
                Text(install.message, color = Coral, fontSize = 16.sp)
            }
            InstallState.Done -> {
                Spacer(Modifier.height(16.dp))
                Text("Installed", color = Lime, fontSize = 16.sp)
            }
            else -> {}
        }

        if (app.description.isNotBlank()) {
            Spacer(Modifier.height(28.dp))
            Text(app.description, color = Fog, fontSize = 17.sp, lineHeight = 26.sp, modifier = Modifier.widthIn(max = 900.dp))
        }

        if (app.versions.size > 1 || app.versions.firstOrNull()?.changelog?.isNotBlank() == true) {
            Spacer(Modifier.height(32.dp))
            Text("Versions", color = Fog, fontSize = 20.sp, fontWeight = FontWeight.SemiBold)
            Spacer(Modifier.height(8.dp))
            for (v in app.versions) {
                Row(Modifier.fillMaxWidth().padding(vertical = 6.dp), verticalAlignment = Alignment.CenterVertically) {
                    Column(Modifier.weight(1f)) {
                        Text("${v.versionName} (${v.versionCode}) · ${humanSize(v.size)} · min Android API ${v.minSdk}", color = Fog, fontSize = 15.sp)
                        if (v.changelog.isNotBlank()) Text(v.changelog, color = Mist, fontSize = 14.sp)
                    }
                    if (v.versionCode != app.latest?.versionCode) {
                        OutlinedButton(onClick = { vm.clearInstallState(pkg); vm.install(app, v.versionCode) }, enabled = !busy) { Text("Install") }
                    }
                }
            }
        }
    }
}

// ---------------------------------------------------------------- Settings

@Composable
fun SettingsScreen(vm: StoreViewModel) {
    val fr = remember { FocusRequester() }
    LaunchedEffect(Unit) { runCatching { fr.requestFocus() } }
    Column(Modifier.fillMaxSize().padding(Gutter)) {
        Text("Settings", style = MaterialTheme.typography.headlineMedium, color = Fog)
        Spacer(Modifier.height(24.dp))
        Text("Server", color = Mist, fontSize = 14.sp)
        Text(vm.prefs.serverUrl, color = Fog, fontSize = 18.sp)
        vm.serverInfo?.let { Text("${it.name} · atvloader ${it.version}", color = Mist, fontSize = 14.sp) }
        Spacer(Modifier.height(20.dp))
        Text("This TV", color = Mist, fontSize = 14.sp)
        Text(vm.prefs.deviceName.ifBlank { "Linked" }, color = Fog, fontSize = 18.sp)
        Spacer(Modifier.height(32.dp))
        Row {
            Button(onClick = { vm.changeServer() }, modifier = Modifier.focusRequester(fr)) { Text("Change server") }
            Spacer(Modifier.width(12.dp))
            OutlinedButton(onClick = { vm.unlink() }) { Text("Unlink this TV") }
            Spacer(Modifier.width(12.dp))
            OutlinedButton(onClick = { vm.openUnknownSourcesSettings() }) { Text("Unknown sources setting") }
        }
    }
}
