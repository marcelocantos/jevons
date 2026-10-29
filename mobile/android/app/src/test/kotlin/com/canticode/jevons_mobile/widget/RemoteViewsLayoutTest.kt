package com.canticode.jevons_mobile.widget

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.w3c.dom.Element
import java.io.File
import javax.xml.parsers.DocumentBuilderFactory

/** Static guard for the widget's RemoteViews resources. JVM tests cannot run RemoteViews.apply; the Fold is the runtime oracle. */
class RemoteViewsLayoutTest {
    private val widgetLayouts = listOf("usage_widget", "usage_column", "usage_bar", "usage_spacer", "usage_widget_preview")

    @Test fun widgetIsOfferedOnTheLockScreenAndPickerPreviewIsMarked() {
        val info = File("src/main/res/xml/usage_widget.xml").readText()
        assertTrue(info.contains("android:widgetCategory=\"keyguard|home_screen\""))
        assertTrue("no host-driven polling", info.contains("android:updatePeriodMillis=\"0\""))
        assertTrue(File("src/main/res/layout/usage_widget_preview.xml").readText().contains("PREVIEW ONLY · NOT LIVE USAGE"))
    }

    @Test fun manifestRegistersTheWidgetWithoutAPairingPath() {
        val text = File("src/main/AndroidManifest.xml").readText()
        assertTrue(text.contains("android:name=\".widget.UsageWidget\""))
        assertTrue("upgrade receiver missing", text.contains("android.intent.action.MY_PACKAGE_REPLACED"))
        assertFalse("one app, one auth process", text.contains("Pairing"))
    }

    @Test fun everyWidgetLayoutContainsOnlyRemoteViewsInflatableClasses() {
        val allowed = setOf("FrameLayout", "LinearLayout", "RelativeLayout", "GridLayout", "TextView", "ImageView",
            "ProgressBar", "Button", "ImageButton", "Chronometer", "AnalogClock", "ViewFlipper", "StackView",
            "AdapterViewFlipper", "ListView", "GridView")
        for (name in widgetLayouts) {
            val file = File("src/main/res/layout/$name.xml")
            fun check(element: Element) {
                assertTrue("${file.name}: <${element.tagName}> cannot inflate via RemoteViews", element.tagName in allowed)
                val children = element.childNodes
                for (i in 0 until children.length) (children.item(i) as? Element)?.let(::check)
            }
            check(DocumentBuilderFactory.newInstance().newDocumentBuilder().parse(file).documentElement)
        }
    }
}
