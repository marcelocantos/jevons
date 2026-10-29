// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package com.canticode.jevons_mobile.widget

import org.json.JSONObject
import java.time.Instant

/** What one GET /api/plan-usage told the widget. */
sealed interface FetchOutcome {
    data class Reading(val payload: String) : FetchOutcome
    /** The ngrok sign-in gate answered instead of jevonsd: the app's session is missing or expired. */
    data object SignedOut : FetchOutcome
    data class Failed(val reason: String) : FetchOutcome
}

/** The last reading the widget fetched, kept so a signed-out widget can still show it with its age. */
data class CachedReading(val payload: String, val savedAt: Instant)

data class WidgetView(val header: String, val rows: List<UsageRow>, val signedOut: Boolean)

/** Pure decisions behind the widget: endpoint, response classification, and what to show. */
object UsageView {
    const val SIGN_IN = "Open Jevons to sign in"

    fun endpoint(cockpitUrl: String): String = cockpitUrl.trim().trimEnd('/') + "/api/plan-usage"

    /**
     * Redirects are not followed. With no session, ngrok answers 302 to its
     * identity provider; following it would fetch a login page, not usage.
     */
    fun classify(code: Int, contentType: String?, body: String?): FetchOutcome = when {
        code in 300..399 || code == 401 || code == 403 -> FetchOutcome.SignedOut
        code != 200 -> FetchOutcome.Failed("HTTP $code")
        contentType?.startsWith("text/html") == true -> FetchOutcome.SignedOut
        body == null -> FetchOutcome.Failed("empty response")
        runCatching { JSONObject(body) }.isFailure -> FetchOutcome.Failed("malformed response")
        else -> FetchOutcome.Reading(body)
    }

    /** " · last reading 23m old", or " · last reading just now"; empty with no reading. */
    fun lastReadingSuffix(cached: CachedReading?, now: Instant): String {
        val age = cached?.let { UsageRows.relativeAge(it.savedAt, now) } ?: return ""
        return if (age == "now") " · last reading just now" else " · last reading $age old"
    }

    fun compose(outcome: FetchOutcome, cached: CachedReading?, now: Instant): WidgetView {
        val lastReading = lastReadingSuffix(cached, now)
        val lastRows = cached?.let { runCatching { UsageRows.fromPayload(it.payload, now, forceStale = true) }.getOrNull() }.orEmpty()
        return when (outcome) {
            is FetchOutcome.Reading -> {
                val rows = UsageRows.fromPayload(outcome.payload, now)
                WidgetView(if (rows.isEmpty()) "Jevons usage · none published yet" else "Jevons usage", rows, signedOut = false)
            }
            FetchOutcome.SignedOut -> WidgetView(SIGN_IN + lastReading, lastRows, signedOut = true)
            is FetchOutcome.Failed -> WidgetView("Jevons unreachable$lastReading", lastRows, signedOut = false)
        }
    }
}
