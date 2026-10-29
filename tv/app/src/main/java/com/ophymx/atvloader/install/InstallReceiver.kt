package com.ophymx.atvloader.install

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

class InstallReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        Installer.onStatus(context, intent)
    }
}
