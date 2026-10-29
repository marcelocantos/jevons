// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package com.canticode.jevons_mobile.widget

import android.content.Context

/** sqlpipe's embedded Database, shared by the Flutter settings and the widget. */
class DeviceStore(context: Context) {
    private val context = context.applicationContext
    private val path = context.getDatabasePath("jevons-device.sqlite").absolutePath

    init { context.getDatabasePath("jevons-device.sqlite").parentFile?.mkdirs() }

    fun get(key: String): String? {
        val stored = getNative(path, key)
        if (stored != null) return stored
        if (key == "cockpit_url") {
            // One-time migration from the old Flutter shared_preferences backend.
            val legacy = context.getSharedPreferences("FlutterSharedPreferences", Context.MODE_PRIVATE)
            val value = legacy.getString("flutter.cockpit_url", null) ?: return null
            put(key, value)
            legacy.edit().remove("flutter.cockpit_url").apply()
            return value
        }
        if (key == "widget_payload" || key == "widget_saved_at_ms") {
            val legacy = context.getSharedPreferences("jevons_usage_widget", Context.MODE_PRIVATE)
            val payload = legacy.getString("payload", null) ?: return null
            val timestamp = legacy.getLong("saved_at_ms", 0L).takeIf { it > 0 } ?: return null
            putReading(payload, timestamp)
            legacy.edit().remove("payload").remove("saved_at_ms").apply()
            return if (key == "widget_payload") payload else timestamp.toString()
        }
        return null
    }

    fun put(key: String, value: String) = putNative(path, key, value)
    fun putReading(payload: String, timestamp: Long) = putReadingNative(path, payload, timestamp)

    private external fun getNative(path: String, key: String): String?
    private external fun putNative(path: String, key: String, value: String)
    private external fun putReadingNative(path: String, payload: String, timestamp: Long)

    companion object { init { System.loadLibrary("jevons_store") } }
}
