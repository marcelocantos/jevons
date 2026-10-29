// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package com.canticode.jevons_mobile.widget

import org.json.JSONObject
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale
import kotlin.math.max

/**
 * [exhausted]: the server's band says the window is spent; drawn as the cockpit's red border.
 * [elapsedPercent]: the share of the window's period that has elapsed in time (not usage),
 * the cockpit's chevron (🎯T673);
 * null when the period is unknown, so no position is invented.
 */
data class UsageBar(
    val label: String, val fillPercent: Int?, val colour: Int, val reset: String,
    val exhausted: Boolean = false, val elapsedPercent: Int? = null,
)
data class UsageRow(val provider: String, val bars: List<UsageBar>, val stale: String?)

/** Pure projection of the /api/plan-usage wire snapshot. No client-side pace classification. */
object UsageRows {
    const val OK = 0xff7acb7a.toInt()
    const val ELEVATED = 0xffffc45b.toInt()
    const val CRITICAL = 0xffff6969.toInt()
    const val UNKNOWN = 0xffa0a0a0.toInt()
    private const val MAX_AGE_SECONDS = 15 * 60L

    // ui/src/plan/windowGeom.ts limitSecondsFor: the published length, else these by name.
    private const val SESSION_LIMIT_SECONDS = 5 * 60 * 60L
    private const val WEEKLY_LIMIT_SECONDS = 7 * 24 * 60 * 60L
    private const val MONTHLY_LIMIT_SECONDS = 30 * 24 * 60 * 60L

    /** Percent of the window's period elapsed in time at [now] (not usage), as the cockpit's chevron places it. */
    fun elapsedPercent(name: String, limitSeconds: Long?, resetsAt: String, now: Instant): Int? {
        val reset = runCatching { Instant.parse(resetsAt) }.getOrNull() ?: return null
        val limit = limitSeconds?.takeIf { it > 0 } ?: when (name) {
            "session" -> SESSION_LIMIT_SECONDS
            "weekly" -> WEEKLY_LIMIT_SECONDS
            "monthly" -> MONTHLY_LIMIT_SECONDS
            else -> return null
        }
        val remaining = (100.0 * (reset.epochSecond - now.epochSecond) / limit).coerceIn(0.0, 100.0)
        return (100 - remaining).toInt()
    }

    /** Owner 2026-10-29: Bedrock is work-only; the personal widget never shows it. */
    private val HIDDEN_PROVIDERS = setOf("bedrock")

    /** ui/src/relTime.ts: minutes, hours, days, then local calendar labels. */
    fun relativeAge(fetched: Instant, now: Instant): String {
        val seconds = max(0, now.epochSecond - fetched.epochSecond)
        if (seconds < 60) return "now"
        val minutes = seconds / 60
        if (minutes < 60) return "${minutes}m"
        val hours = minutes / 60
        if (hours < 24) return "${hours}h"
        val days = hours / 24
        if (days < 7) return "${days}d"
        val local = fetched.atZone(ZoneId.systemDefault())
        val year = now.atZone(ZoneId.systemDefault()).year
        val pattern = if (local.year != year) "M/d/yy"
            else if (days < 14) "EE HH:mm" else "MMM d"
        return local.format(DateTimeFormatter.ofPattern(pattern, Locale.ENGLISH))
    }

    /**
     * [forceStale] is set when the widget could not refresh (signed out,
     * unreachable): the last reading stays visible but greyed, as the cockpit
     * paints last-success quantities, so it never reads as current capacity.
     */
    fun fromPayload(payload: String, now: Instant, forceStale: Boolean = false): List<UsageRow> {
        val root = JSONObject(payload)
        if (root.optBoolean("disabled") || root.optBoolean("pending") || root.optString("error").isNotEmpty()) return emptyList()
        val backends = root.optJSONArray("backends") ?: return emptyList()
        return (0 until backends.length()).mapNotNull { index ->
            val be = backends.optJSONObject(index) ?: return@mapNotNull null
            val provider = be.optString("provider").takeIf { it.isNotBlank() } ?: return@mapNotNull null
            if (provider.lowercase() in HIDDEN_PROVIDERS) return@mapNotNull null
            val fetched = runCatching { Instant.parse(be.getString("fetched_at")) }.getOrNull()
            val age = fetched?.let { max(0, now.epochSecond - it.epochSecond) }
            val stale = forceStale || age == null || age >= MAX_AGE_SECONDS || be.optBoolean("stale") ||
                be.optString("status") != "available" ||
                runCatching { Instant.parse(be.getString("stale_at")) }.getOrNull()?.let { !now.isBefore(it) } == true
            val windows = be.optJSONArray("windows")
            val override = be.optJSONObject("override")?.optString("band")?.takeIf { it.isNotBlank() }
            val bars = (0 until (windows?.length() ?: 0)).mapNotNull { i ->
                val w = windows?.optJSONObject(i) ?: return@mapNotNull null
                val model = w.optString("model").trim()
                val name = w.optString("name").lowercase()
                val label = if (model.isNotEmpty()) model.first().uppercase() else when (name) {
                    "session" -> "S"; "weekly" -> "W"; "monthly" -> "M"
                    else -> name.firstOrNull()?.uppercase() ?: "?"
                }
                val rem = w.optDouble("remaining_percent", Double.NaN).takeIf { it.isFinite() }
                val used = w.optDouble("used_percent", Double.NaN).takeIf { it.isFinite() }
                // T670: spent (remaining <= 0) is an EMPTY critical bar, not a full one.
                // A window with no number has no fill at all, never a fabricated 0%.
                val fill = if (rem != null && rem <= 0) 0
                    else (used ?: rem?.let { 100 - it })?.coerceIn(0.0, 100.0)?.toInt()
                val band = override ?: w.optString("band")
                val colour = if (stale || fill == null) UNKNOWN else when (band) {
                    "hot", "exhausted" -> CRITICAL
                    "ahead", "under" -> ELEVATED
                    "ok", "locked" -> OK
                    else -> UNKNOWN
                }
                // A stale reading keeps a neutral border: it must never look like current capacity.
                val limitSeconds = w.optLong("limit_window_seconds", 0L).takeIf { it > 0 }
                UsageBar(label, fill, colour, w.optString("resets_at").ifEmpty { "reset unknown" },
                    exhausted = !stale && band == "exhausted",
                    elapsedPercent = elapsedPercent(name, limitSeconds, w.optString("resets_at"), now))
            }
            // Under forceStale the widget header carries the reading's age.
            UsageRow(provider, bars, when {
                // A backend that publishes nothing (its fetched_at is the zero time) has no age to report.
                be.optString("status") == "unavailable" && bars.isEmpty() -> "unavailable"
                stale && !forceStale && fetched != null -> "last known · ${relativeAge(fetched, now)} old"
                stale && !forceStale -> "stale · age unknown"
                bars.isEmpty() || bars.all { it.fillPercent == null } -> "no published window"
                else -> null
            })
        }
    }
}
