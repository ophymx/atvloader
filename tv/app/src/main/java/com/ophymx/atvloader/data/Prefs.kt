package com.ophymx.atvloader.data

import android.content.Context
import androidx.core.content.edit

/** Tiny settings holder: where the store is and the device token it gave us. */
class Prefs(context: Context) {
    private val sp = context.getSharedPreferences("atvloader", Context.MODE_PRIVATE)

    var serverUrl: String
        get() = sp.getString("serverUrl", "") ?: ""
        set(v) = sp.edit { putString("serverUrl", v.trim().trimEnd('/')) }

    var deviceToken: String?
        get() = sp.getString("deviceToken", null)
        set(v) = sp.edit { if (v == null) remove("deviceToken") else putString("deviceToken", v) }

    var deviceName: String
        get() = sp.getString("deviceName", "") ?: ""
        set(v) = sp.edit { putString("deviceName", v) }

    fun clearDevice() = sp.edit { remove("deviceToken"); remove("deviceName") }
}
