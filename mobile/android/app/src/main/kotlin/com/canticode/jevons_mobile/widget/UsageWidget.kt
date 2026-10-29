// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package com.canticode.jevons_mobile.widget

import android.app.PendingIntent
import android.app.job.JobInfo
import android.app.job.JobScheduler
import android.app.job.JobParameters
import android.app.job.JobService
import android.appwidget.AppWidgetManager
import android.appwidget.AppWidgetProvider
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.util.Log
import android.webkit.CookieManager
import androidx.annotation.RequiresApi
import com.canticode.jevons_mobile.MainActivity
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL
import java.time.Instant
import java.util.concurrent.CompletableFuture
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

/**
 * Lock-screen / home-screen plan-usage widget. It reads the cockpit with the
 * session the app's WebView already holds; it has no credential of its own.
 */
class UsageWidget : AppWidgetProvider() {
    override fun onUpdate(context: Context, manager: AppWidgetManager, ids: IntArray) {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.S) return
        val cached = ReadingStore(context).load()
        val now = Instant.now()
        val lastRows = cached?.let { runCatching { UsageRows.fromPayload(it.payload, now, forceStale = true) }.getOrNull() }.orEmpty()
        val age = UsageView.lastReadingSuffix(cached, now)
        publish(context, WidgetView("Jevons usage · refreshing$age", lastRows, signedOut = false))
        UsageJobs.schedulePeriodic(context)
        // A newly placed widget should not wait a whole period for its first reading.
        refreshNow(context)
    }

    override fun onReceive(context: Context, intent: Intent) {
        super.onReceive(context, intent)
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.S) return
        when (intent.action) {
            // Hosts can keep the prior RemoteViews across an APK update; re-publish placed instances.
            Intent.ACTION_MY_PACKAGE_REPLACED -> {
                val manager = AppWidgetManager.getInstance(context)
                val ids = placedIds(context)
                if (ids.isNotEmpty()) onUpdate(context, manager, ids)
            }
        }
    }

    override fun onDisabled(context: Context) {
        UsageJobs.cancel(context)
    }

    companion object {
        /**
         * Mirrors kDefaultCockpitUrl in lib/settings.dart, the one place the
         * default is chosen; UsageViewTest fails if the two drift.
         */
        const val DEFAULT_COCKPIT_URL = "https://jevons.canticode.com"

        fun cockpitUrl(context: Context): String =
            DeviceStore(context).get("cockpit_url")?.trim()?.takeIf { it.isNotEmpty() } ?: DEFAULT_COCKPIT_URL

        fun placedIds(context: Context): IntArray =
            AppWidgetManager.getInstance(context).getAppWidgetIds(ComponentName(context, UsageWidget::class.java))

        /** Opening the app replaces any pending retry, so a backoff cannot swallow sign-in. */
        fun refreshNow(context: Context) {
            UsageJobs.scheduleNow(context)
        }

        @RequiresApi(Build.VERSION_CODES.S)
        fun publish(context: Context, view: WidgetView) {
            val manager = AppWidgetManager.getInstance(context)
            // A tap opens Jevons (owner 2026-10-29), where sign-in also happens;
            // MainActivity refreshes the widget as it starts.
            val tap = PendingIntent.getActivity(context, 1, Intent(context, MainActivity::class.java)
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK), PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
            for (id in placedIds(context)) {
                val height = manager.getAppWidgetOptions(id).getInt(AppWidgetManager.OPTION_APPWIDGET_MIN_HEIGHT, 90)
                manager.updateAppWidget(id, UsageRender.render(context, view, height, tap))
            }
        }
    }
}

/** The last successful reading and its timestamp share the sqlpipe device database. */
class ReadingStore(context: Context) {
    private val store = DeviceStore(context)

    fun load(): CachedReading? {
        val payload = store.get("widget_payload") ?: return null
        val savedAt = store.get("widget_saved_at_ms")?.toLongOrNull()?.takeIf { it > 0 } ?: return null
        return CachedReading(payload, Instant.ofEpochMilli(savedAt))
    }

    fun save(reading: CachedReading) {
        store.putReading(reading.payload, reading.savedAt.toEpochMilli())
    }
}

/** Platform scheduler: no WorkManager (and therefore no Room). */
object UsageJobs {
    const val PERIODIC_ID = 128201
    const val NOW_ID = 128202
    const val PERIOD_MS = 15 * 60 * 1000L
    private fun scheduler(context: Context) = context.getSystemService(JobScheduler::class.java)
    private fun builder(context: Context, id: Int) = JobInfo.Builder(
        id, ComponentName(context, UsageJobService::class.java)
    ).setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY).setBackoffCriteria(
        5 * 60 * 1000L, JobInfo.BACKOFF_POLICY_EXPONENTIAL
    )

    fun schedulePeriodic(context: Context) {
        if (scheduler(context).allPendingJobs.none { it.id == PERIODIC_ID }) {
            scheduler(context).schedule(builder(context, PERIODIC_ID).setPersisted(true).setPeriodic(PERIOD_MS).build())
        }
    }

    fun scheduleNow(context: Context) {
        // Same id replaces a job waiting out exponential backoff: opening the
        // app after sign-in must fetch now, not be swallowed by that retry.
        scheduler(context).schedule(builder(context, NOW_ID).build())
    }

    fun cancel(context: Context) {
        scheduler(context).cancel(PERIODIC_ID)
        scheduler(context).cancel(NOW_ID)
    }
}

class UsageJobService : JobService() {
    private val executor = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())
    // Android calls JobService lifecycle methods on the main thread. A periodic
    // tick and a tap can arrive together: only one fetch should use the cookie.
    private var active: JobParameters? = null
    override fun onStartJob(params: JobParameters): Boolean {
        if (active != null) return false
        active = params
        executor.execute {
            val retry = runCatching { UsageFetcher(applicationContext).run() }.getOrElse {
                Log.e("JevonsUsageWidget", "fetch failed", it)
                true
            }
            main.post {
                if (active === params) {
                    active = null
                    jobFinished(params, retry)
                }
            }
        }
        return true
    }
    override fun onStopJob(params: JobParameters): Boolean {
        if (active === params) active = null
        return true // interrupted fetch: retry with backoff
    }
    override fun onDestroy() {
        executor.shutdownNow()
        super.onDestroy()
    }
}

/** One GET /api/plan-usage with the WebView's cookies for the cockpit origin. */
class UsageFetcher(private val context: Context) {
    /** True asks JobScheduler to retry with its exponential backoff. */
    fun run(): Boolean {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.S) return false
        if (UsageWidget.placedIds(context).isEmpty()) {
            Log.i(TAG, "plan-usage fetch skipped: no widget placed")
            return false
        }
        val url = UsageView.endpoint(UsageWidget.cockpitUrl(context))
        // The app's sign-in lives in the WebView cookie store; read it on the main thread.
        val cookie = if (Looper.myLooper() == Looper.getMainLooper()) {
            CookieManager.getInstance().getCookie(url)
        } else {
            val reply = CompletableFuture<String?>()
            Handler(Looper.getMainLooper()).post {
                try { reply.complete(CookieManager.getInstance().getCookie(url)) }
                catch (e: Exception) { reply.completeExceptionally(e) }
            }
            reply.get(10, TimeUnit.SECONDS)
        }
        val outcome = fetch(url, cookie)
        val store = ReadingStore(context)
        val now = Instant.now()
        if (outcome is FetchOutcome.Reading) store.save(CachedReading(outcome.payload, now))
        val view = UsageView.compose(outcome, store.load(), now)
        UsageWidget.publish(context, view)
        Log.i(TAG, "plan-usage fetch: $outcome rows=${view.rows.size} cookie=${!cookie.isNullOrEmpty()}".take(LOG_LIMIT))
        // Signed out is not retried: nothing changes until the owner signs in
        // in the app, which triggers its own refresh. Failures back off.
        return outcome is FetchOutcome.Failed
    }

    private fun fetch(url: String, cookie: String?): FetchOutcome = try {
        val conn = URL(url).openConnection() as HttpURLConnection
        try {
            conn.instanceFollowRedirects = false
            conn.connectTimeout = CONNECT_TIMEOUT_MS
            // jevonsd long-polls GET /api/plan-usage until its first batch lands.
            conn.readTimeout = READ_TIMEOUT_MS
            conn.setRequestProperty("Accept", "application/json")
            if (!cookie.isNullOrEmpty()) conn.setRequestProperty("Cookie", cookie)
            val code = conn.responseCode
            val body = if (code == HttpURLConnection.HTTP_OK) conn.inputStream.bufferedReader().use { it.readText() } else null
            UsageView.classify(code, conn.contentType, body)
        } finally {
            conn.disconnect()
        }
    } catch (e: IOException) {
        FetchOutcome.Failed(e.javaClass.simpleName)
    }

    private companion object {
        const val TAG = "JevonsUsageWidget"
        const val LOG_LIMIT = 200 // a Reading's toString carries the payload; log only its head
        const val CONNECT_TIMEOUT_MS = 10_000
        const val READ_TIMEOUT_MS = 40_000
    }
}
