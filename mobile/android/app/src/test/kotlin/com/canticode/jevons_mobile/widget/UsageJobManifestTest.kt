// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package com.canticode.jevons_mobile.widget

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/** JobScheduler rejects network-constrained jobs at runtime without this permission. */
class UsageJobManifestTest {
    @Test fun jobSchedulerNetworkConstraintHasManifestPermission() {
        val widget = File("src/main/kotlin/com/canticode/jevons_mobile/widget/UsageWidget.kt").readText()
        assertTrue("guard must cover the real widget scheduler", widget.contains("setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY)"))
        val manifest = File("src/main/AndroidManifest.xml").readText()
        assertTrue("otherwise scheduling crashes the widget and app", manifest.contains(
            "android.permission.ACCESS_NETWORK_STATE"
        ))
    }
}
