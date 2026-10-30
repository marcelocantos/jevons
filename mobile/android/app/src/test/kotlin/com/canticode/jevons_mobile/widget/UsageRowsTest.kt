package com.canticode.jevons_mobile.widget

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Instant

class UsageRowsTest {
    private val now = Instant.parse("2026-10-28T00:00:00Z")

    @Test fun labelsFillAndOverride() {
        val rows = UsageRows.fromPayload("""{"backends":[{"provider":"claude","status":"available","fetched_at":"2026-10-27T23:59:00Z","override":{"band":"ok"},"windows":[{"name":"session","remaining_percent":20,"band":"hot","resets_at":"2026-10-28T01:00:00Z"},{"name":"weekly","remaining_percent":50,"used_percent":55,"band":"ok"},{"name":"weekly_model","model":"Fable","remaining_percent":0,"band":"exhausted"}]}]}""", now)
        assertEquals(listOf("S", "W", "F"), rows.single().bars.map { it.label })
        assertEquals(listOf(80, 55, 0), rows.single().bars.map { it.fillPercent })
        // The owner's override band wins over the server's per-window band.
        assertEquals(listOf(UsageRows.OK, UsageRows.OK, UsageRows.OK), rows.single().bars.map { it.colour })
        assertEquals("2026-10-28T01:00:00Z", rows.single().bars.first().reset)
    }

    @Test fun serverBandColoursEachWindow() {
        val rows = UsageRows.fromPayload("""{"backends":[{"provider":"codex","status":"available","fetched_at":"2026-10-27T23:59:00Z","windows":[{"name":"session","used_percent":95,"band":"hot"},{"name":"weekly","used_percent":60,"band":"ahead"},{"name":"monthly","used_percent":10,"band":"ok"}]}]}""", now)
        assertEquals(listOf("S", "W", "M"), rows.single().bars.map { it.label })
        assertEquals(listOf(UsageRows.CRITICAL, UsageRows.ELEVATED, UsageRows.OK), rows.single().bars.map { it.colour })
        assertNull(rows.single().stale)
    }

    @Test fun underPaceIsTheCockpitBlueNotAWarning() {
        // Fable 34% used with ~57% of the week gone: spare capacity, blue in the cockpit.
        val rows = UsageRows.fromPayload("""{"backends":[{"provider":"claude","status":"available","fetched_at":"2026-10-27T23:59:00Z","windows":[{"name":"weekly_model","model":"Fable","used_percent":34,"band":"under"},{"name":"weekly","used_percent":80,"band":"ahead"}]}]}""", now)
        assertEquals(listOf(UsageRows.UNDER, UsageRows.ELEVATED), rows.single().bars.map { it.colour })
    }

    @Test fun lockedIsItsOwnColourNotHealthy() {
        val rows = UsageRows.fromPayload("""{"backends":[{"provider":"claude","status":"available","fetched_at":"2026-10-27T23:59:00Z","windows":[{"name":"weekly","used_percent":40,"band":"locked"},{"name":"session","used_percent":10,"band":"ok"}]}]}""", now)
        assertEquals(listOf(UsageRows.LOCKED, UsageRows.OK), rows.single().bars.map { it.colour })
    }

    @Test fun staleKeepsLastQuantityGreyedAndMissingWindowHasNoFill() {
        val rows = UsageRows.fromPayload("""{"backends":[{"provider":"grok","status":"available","fetched_at":"2026-10-27T23:30:00Z","windows":[{"name":"weekly","remaining_percent":90,"band":"ok"},{"name":"session","band":"ok"}]},{"provider":"cursor","status":"available","fetched_at":"2026-10-27T23:59:00Z","windows":[]}]}""", now)
        val grok = rows[0]
        assertEquals(10, grok.bars[0].fillPercent)
        assertEquals(UsageRows.UNKNOWN, grok.bars[0].colour)
        assertNull("a window with no number is never a fabricated 0%", grok.bars[1].fillPercent)
        assertEquals("last known · 30m old", grok.stale)
        assertEquals("no published window", rows[1].stale)
        assertTrue(rows[1].bars.isEmpty())
    }

    @Test fun forceStaleGreysAFreshReading() {
        val payload = """{"backends":[{"provider":"claude","status":"available","fetched_at":"2026-10-27T23:59:00Z","windows":[{"name":"session","used_percent":40,"band":"hot"}]}]}"""
        assertEquals(UsageRows.CRITICAL, UsageRows.fromPayload(payload, now).single().bars.single().colour)
        val forced = UsageRows.fromPayload(payload, now, forceStale = true).single()
        assertEquals(UsageRows.UNKNOWN, forced.bars.single().colour)
        assertEquals(40, forced.bars.single().fillPercent)
        assertNull("the widget header carries the age", forced.stale)
    }

    @Test fun staleAgeUsesCockpitScaleNotUnboundedMinutes() {
        val older = { seconds: Long -> now.minusSeconds(seconds) }
        assertEquals("2h", UsageRows.relativeAge(older(2 * 3600), now))
        assertEquals("3d", UsageRows.relativeAge(older(3 * 86400), now))
        assertFalse(UsageRows.relativeAge(older(432054 * 60L), now).endsWith("m"))
    }

    @Test fun capturedRealPayloadCoversEveryBackendIncludingCursor() {
        val json = javaClass.classLoader!!.getResource("plan_usage_capture.json")!!.readText()
        val rows = UsageRows.fromPayload(json, Instant.parse(JSONObject(json).getString("at")))
        assertEquals("bedrock is work-only and left out", listOf("claude", "codex", "grok", "cursor"), rows.map { it.provider })
        assertEquals("per-model window takes the model initial", listOf("S", "W", "F"), rows[0].bars.map { it.label })
        assertEquals(listOf("M", "A"), rows[3].bars.map { it.label })
        assertEquals("spent window is an empty bar", 0, rows[3].bars[1].fillPercent)
    }

    @Test fun exhaustedWindowGetsTheCockpitBorderOnlyWhileCurrent() {
        val rows = UsageRows.fromPayload("""{"backends":[{"provider":"cursor","status":"available","fetched_at":"2026-10-27T23:59:00Z","windows":[{"name":"monthly","used_percent":44,"band":"ok"},{"name":"api","model":"API","used_percent":100,"remaining_percent":0,"band":"exhausted"}]}]}""", now)
        val (monthly, api) = rows.single().bars
        assertFalse(monthly.exhausted)
        assertTrue(api.exhausted)
        assertEquals(0, api.fillPercent)
        val stale = UsageRows.fromPayload("""{"backends":[{"provider":"cursor","status":"available","fetched_at":"2026-10-27T23:00:00Z","windows":[{"name":"api","remaining_percent":0,"band":"exhausted"}]}]}""", now)
        assertFalse("a stale spent window never looks current", stale.single().bars.single().exhausted)
    }

    @Test fun unavailableBackendSaysSoInsteadOfAnAge() {
        val rows = UsageRows.fromPayload("""{"backends":[{"provider":"ollama","status":"unavailable","fetched_at":"0001-01-01T00:00:00Z","windows":[]}]}""", now)
        assertEquals("unavailable", rows.single().stale)
    }

    @Test fun chevronMarksHowFarThroughItsPeriodEachWindowIs() {
        val rows = UsageRows.fromPayload("""{"backends":[{"provider":"claude","status":"available","fetched_at":"2026-10-27T23:59:00Z","windows":[
            {"name":"weekly","used_percent":40,"band":"ok","resets_at":"2026-10-30T00:00:00Z","limit_window_seconds":604800},
            {"name":"session","used_percent":10,"band":"ok","resets_at":"2026-10-28T04:00:00Z"},
            {"name":"weekly_model","model":"Fable","used_percent":10,"band":"ok","resets_at":"2026-10-30T00:00:00Z"},
            {"name":"monthly","used_percent":10,"band":"ok"}]}]}""", now)
        val (weekly, session, model, monthly) = rows.single().bars
        assertEquals("2 of 7 days left", 71, weekly.elapsedPercent)
        assertEquals("session falls back to 5h by name", 20, session.elapsedPercent)
        assertNull("no published length and no fallback name: no chevron", model.elapsedPercent)
        assertNull("no reset: no chevron", monthly.elapsedPercent)
    }

    @Test fun pendingAndDisabledAreNotReadings() {
        assertTrue(UsageRows.fromPayload("""{"pending":true}""", now).isEmpty())
        assertTrue(UsageRows.fromPayload("""{"disabled":true,"error":"plan usage not enabled"}""", now).isEmpty())
    }
}
