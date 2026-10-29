package com.ophymx.atvloader.data

import com.google.gson.annotations.SerializedName

data class ServerInfo(
    val service: String = "",
    val name: String = "",
    val version: String = "",
    val apps: Int = 0,
)

data class VersionInfo(
    val versionCode: Long = 0,
    val versionName: String = "",
    val size: Long = 0,
    val sha256: String = "",
    val minSdk: Int = 0,
    val targetSdk: Int = 0,
    val changelog: String = "",
    val downloadUrl: String = "",
)

data class AppInfo(
    @SerializedName("package") val pkg: String = "",
    val name: String = "",
    val description: String = "",
    val category: String = "",
    val featured: Boolean = false,
    val iconUrl: String? = null,
    val latest: VersionInfo? = null,
    val versions: List<VersionInfo> = emptyList(),
)

data class AppsResponse(
    val apps: List<AppInfo> = emptyList(),
    val categories: List<String> = emptyList(),
)

data class DeviceCode(
    val deviceCode: String = "",
    val userCode: String = "",
    val expiresIn: Int = 0,
    val interval: Int = 5,
)

data class DeviceToken(
    val status: String = "",
    val token: String? = null,
    val deviceName: String? = null,
)

data class ApiError(val error: String = "", val status: Int = 0)
