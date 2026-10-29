@file:OptIn(ExperimentalTvMaterial3Api::class)

package com.ophymx.atvloader

import androidx.tv.material3.ExperimentalTvMaterial3Api
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.lifecycle.viewmodel.compose.viewModel
import coil3.ImageLoader
import coil3.network.okhttp.OkHttpNetworkFetcherFactory
import com.ophymx.atvloader.ui.AtvLoaderTheme
import com.ophymx.atvloader.ui.BrowseScreen
import com.ophymx.atvloader.ui.DetailScreen
import com.ophymx.atvloader.ui.Ink
import com.ophymx.atvloader.ui.Modal
import com.ophymx.atvloader.ui.PairingScreen
import com.ophymx.atvloader.ui.Screen
import com.ophymx.atvloader.ui.SettingsScreen
import com.ophymx.atvloader.ui.SetupScreen
import com.ophymx.atvloader.ui.StoreViewModel

class MainActivity : ComponentActivity() {
    private var vm: StoreViewModel? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            val model: StoreViewModel = viewModel()
            vm = model
            val imageLoader = remember {
                ImageLoader.Builder(this@MainActivity)
                    .components { add(OkHttpNetworkFetcherFactory(callFactory = { model.api.client })) }
                    .build()
            }
            AtvLoaderTheme {
                BackHandler(enabled = model.screen !is Screen.Setup && model.screen !is Screen.Browse) { model.back() }
                Box(Modifier.fillMaxSize().background(Ink)) {
                    when (val s = model.screen) {
                        Screen.Setup -> SetupScreen(model)
                        Screen.Pairing -> PairingScreen(model)
                        Screen.Browse -> BrowseScreen(model, imageLoader)
                        is Screen.Detail -> DetailScreen(model, s.pkg, imageLoader)
                        Screen.Settings -> SettingsScreen(model)
                    }
                    if (model.needsUnknownSources) {
                        Modal(
                            title = "Allow installs from ATV Loader",
                            body = "Android needs your permission once before this app can install APKs. Turn on \"Install unknown apps\" for ATV Loader, then come back and try again.",
                            primary = "Open settings", onPrimary = { model.openUnknownSourcesSettings() },
                            secondary = "Not now", onSecondary = { model.dismissUnknownSources() },
                        )
                    }
                }
            }
        }
    }

    override fun onResume() {
        super.onResume()
        vm?.onResume()
    }
}
