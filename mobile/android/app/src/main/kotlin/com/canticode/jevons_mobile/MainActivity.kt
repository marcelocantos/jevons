package com.canticode.jevons_mobile

import android.os.Build
import android.webkit.CookieManager
import com.canticode.jevons_mobile.widget.UsageWidget
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import com.canticode.jevons_mobile.widget.DeviceStore

class MainActivity : FlutterActivity() {
    override fun configureFlutterEngine(engine: FlutterEngine) {
        super.configureFlutterEngine(engine)
        MethodChannel(engine.dartExecutor.binaryMessenger, "jevons/device_store").setMethodCallHandler { call, result ->
            try {
                val store = DeviceStore(this)
                when (call.method) {
                    "get" -> result.success(store.get(call.argument<String>("key")!!))
                    "put" -> {
                        store.put(call.argument<String>("key")!!, call.argument<String>("value")!!)
                        result.success(null)
                    }
                    else -> result.notImplemented()
                }
            } catch (e: Exception) { result.error("store", e.message, null) }
        }
    }

    override fun onStart() {
        super.onStart()
        // A widget tap opens the app; refresh the widget as it does.
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S && UsageWidget.placedIds(this).isNotEmpty()) {
            UsageWidget.refreshNow(this)
        }
    }

    override fun onStop() {
        super.onStop()
        // Leaving the app is when a fresh sign-in should reach the usage
        // widget: persist the WebView's cookies and refresh it once.
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S && UsageWidget.placedIds(this).isNotEmpty()) {
            CookieManager.getInstance().flush()
            UsageWidget.refreshNow(this)
        }
    }
}
