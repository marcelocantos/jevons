package com.canticode.jevons_mobile.widget

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import java.time.Instant

class UsageViewTest {
    private val now = Instant.parse("2026-10-28T00:00:00Z")
    private val payload = """{"backends":[{"provider":"claude","status":"available","fetched_at":"2026-10-27T23:59:00Z","windows":[{"name":"session","used_percent":30,"band":"ok"}]}]}"""

    @Test fun ngrokSignInGateIsSignedOutNotUsage() {
        assertEquals(FetchOutcome.SignedOut, UsageView.classify(302, "text/html; charset=utf-8", null))
        assertEquals(FetchOutcome.SignedOut, UsageView.classify(401, null, null))
        assertEquals(FetchOutcome.SignedOut, UsageView.classify(200, "text/html", "<html>sign in</html>"))
    }

    @Test fun jsonIsAReadingAndAnythingElseFails() {
        assertEquals(FetchOutcome.Reading(payload), UsageView.classify(200, "application/json", payload))
        assertEquals(FetchOutcome.Failed("HTTP 502"), UsageView.classify(502, "text/plain", null))
        assertEquals(FetchOutcome.Failed("malformed response"), UsageView.classify(200, "application/json", "not json"))
    }

    @Test fun endpointJoinsTheCockpitUrl() {
        assertEquals("https://jevons.canticode.com/api/plan-usage", UsageView.endpoint("https://jevons.canticode.com/"))
        assertEquals("http://host:13705/api/plan-usage", UsageView.endpoint(" http://host:13705 "))
    }

    @Test fun signedOutShowsLastReadingGreyedWithItsAge() {
        val view = UsageView.compose(FetchOutcome.SignedOut, CachedReading(payload, now.minusSeconds(23 * 60)), now)
        assertTrue(view.signedOut)
        assertEquals("Open Jevons to sign in · last reading 23m old", view.header)
        val bar = view.rows.single().bars.single()
        assertEquals(30, bar.fillPercent)
        assertEquals("never painted as current capacity", UsageRows.UNKNOWN, bar.colour)
    }

    @Test fun aFreshLastReadingSaysJustNow() {
        val view = UsageView.compose(FetchOutcome.SignedOut, CachedReading(payload, now.minusSeconds(20)), now)
        assertEquals("Open Jevons to sign in · last reading just now", view.header)
    }

    @Test fun signedOutWithNoReadingShowsNoNumbers() {
        val view = UsageView.compose(FetchOutcome.SignedOut, null, now)
        assertEquals(UsageView.SIGN_IN, view.header)
        assertTrue(view.rows.isEmpty())
    }

    @Test fun freshReadingIsColouredAndUnreachableIsGreyed() {
        val fresh = UsageView.compose(FetchOutcome.Reading(payload), null, now)
        assertEquals("Jevons usage", fresh.header)
        assertFalse(fresh.signedOut)
        assertEquals(UsageRows.OK, fresh.rows.single().bars.single().colour)
        val down = UsageView.compose(FetchOutcome.Failed("IOException"), CachedReading(payload, now.minusSeconds(3600)), now)
        assertEquals("Jevons unreachable · last reading 1h old", down.header)
        assertEquals(UsageRows.UNKNOWN, down.rows.single().bars.single().colour)
    }

    @Test fun defaultCockpitUrlMatchesTheFlutterShell() {
        val settings = File("../../lib/settings.dart").readText()
        assertTrue(settings.contains("const String kDefaultCockpitUrl = '${UsageWidget.DEFAULT_COCKPIT_URL}';"))
    }
}
