// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package com.canticode.jevons_mobile.widget

import android.app.PendingIntent
import android.content.Context
import android.graphics.Color
import android.os.Build
import android.util.TypedValue
import android.view.View
import android.widget.RemoteViews
import androidx.annotation.RequiresApi
import com.canticode.jevons_mobile.R

/** One upright company column per backend, bottom-filled window bars, bottom letters. */
@RequiresApi(Build.VERSION_CODES.S)
object UsageRender {
    /** Half of usage_chevron's 6dp height: the bottom margin that centres its tip on a height. */
    private const val CHEVRON_HALF_HEIGHT_DP = 3f

    private fun companyMark(provider: String): Int = when (provider.lowercase()) {
        "claude", "anthropic", "bedrock" -> R.drawable.company_anthropic
        "grok", "xai" -> R.drawable.company_xai
        "codex", "openai" -> R.drawable.company_openai
        "cursor" -> R.drawable.company_cursor
        else -> 0
    }

    fun render(context: Context, view: WidgetView, heightDp: Int, tap: PendingIntent): RemoteViews {
        val packageName = context.packageName
        val root = RemoteViews(packageName, R.layout.usage_widget)
        root.setOnClickPendingIntent(R.id.usage_root, tap)
        root.setTextViewText(R.id.usage_state, view.header)
        root.removeAllViews(R.id.provider_columns)
        // A compact outer screen uses a shorter ruler; a taller inner widget
        // has breathing room. RemoteViews accepts dp heights from API 31.
        val res = context.resources
        val titleGapDp = (res.getDimension(R.dimen.usage_title_gap) / res.displayMetrics.density).toInt()
        val trackDp = (heightDp - 93 - titleGapDp).coerceIn(18, 92)
        for (row in view.rows) {
            root.addView(R.id.provider_columns, RemoteViews(packageName, R.layout.usage_spacer))
            val column = RemoteViews(packageName, R.layout.usage_column)
            val mark = companyMark(row.provider)
            if (mark != 0) column.setImageViewResource(R.id.company_mark, mark)
            else column.setViewVisibility(R.id.company_mark, View.GONE)
            column.setTextViewText(R.id.provider_name, row.provider)
            column.setTextViewText(R.id.stale_age, row.stale ?: "")
            column.removeAllViews(R.id.window_bars)
            for (bar in row.bars) {
                val barView = RemoteViews(packageName, R.layout.usage_bar)
                barView.setTextViewText(R.id.window_letter, bar.label)
                barView.setContentDescription(R.id.bar_track, "${row.provider} ${bar.label}: ${bar.fillPercent?.let { "$it% used" } ?: "unknown"}; ${bar.reset}")
                barView.setViewLayoutHeight(R.id.bar_track, trackDp.toFloat(), TypedValue.COMPLEX_UNIT_DIP)
                barView.setViewLayoutHeight(R.id.chevron_rail, trackDp.toFloat(), TypedValue.COMPLEX_UNIT_DIP)
                // 🎯T673: the chevron's tip sits at the height of the period's elapsed time.
                // Unknown period: no chevron, the rail keeps its width.
                val elapsed = bar.elapsedPercent
                if (elapsed == null) barView.setViewVisibility(R.id.chevron, View.INVISIBLE)
                else barView.setViewLayoutMargin(R.id.chevron, RemoteViews.MARGIN_BOTTOM,
                    (trackDp * elapsed / 100f - CHEVRON_HALF_HEIGHT_DP).coerceIn(0f, trackDp - 2 * CHEVRON_HALF_HEIGHT_DP),
                    TypedValue.COMPLEX_UNIT_DIP)
                // The cockpit's spent window: a red inset border on an empty track (T1019).
                barView.setInt(R.id.bar_track, "setBackgroundResource",
                    if (bar.exhausted) R.drawable.usage_track_exhausted else R.drawable.usage_track)
                // An unknown value has NO fill, not a fabricated 0%.
                barView.setViewLayoutHeight(R.id.bar_fill, (trackDp * (bar.fillPercent ?: 0) / 100f), TypedValue.COMPLEX_UNIT_DIP)
                barView.setInt(R.id.bar_fill, "setBackgroundColor", if (bar.fillPercent == null) Color.TRANSPARENT else bar.colour)
                column.addView(R.id.window_bars, barView)
            }
            root.addView(R.id.provider_columns, column)
        }
        if (view.rows.isNotEmpty()) root.addView(R.id.provider_columns, RemoteViews(packageName, R.layout.usage_spacer))
        return root
    }
}
