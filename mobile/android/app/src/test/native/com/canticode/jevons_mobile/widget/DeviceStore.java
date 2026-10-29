// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package com.canticode.jevons_mobile.widget;

import java.nio.file.Files;
import java.nio.file.Path;

/** Standalone JNI ABI and sqlpipe persistence smoke; no Android SDK required. */
public class DeviceStore {
    static { System.loadLibrary("jevons_store"); }
    private native String getNative(String path, String key);
    private native void putNative(String path, String key, String value);
    private native void putReadingNative(String path, String payload, long timestamp);

    public static void main(String[] args) throws Exception {
        Path file = Files.createTempFile("jevons-device-smoke", ".sqlite");
        try {
            DeviceStore store = new DeviceStore();
            store.putNative(file.toString(), "cockpit_url", "https://example.test/?q='\uD83D\uDE00'");
            store.putReadingNative(file.toString(), "{\"reading\":\"\uD83D\uDE00\"}", 123456789L);
            if (!"https://example.test/?q='\uD83D\uDE00'".equals(store.getNative(file.toString(), "cockpit_url")))
                throw new AssertionError("URL did not survive sqlpipe reopen");
            if (!"{\"reading\":\"\uD83D\uDE00\"}".equals(store.getNative(file.toString(), "widget_payload")))
                throw new AssertionError("reading did not survive sqlpipe reopen");
            if (!"123456789".equals(store.getNative(file.toString(), "widget_saved_at_ms")))
                throw new AssertionError("timestamp did not survive sqlpipe reopen");
            if (store.getNative(file.toString(), "missing") != null)
                throw new AssertionError("unknown key should be absent");
            System.out.println("sqlpipe JNI device store GREEN");
        } finally { Files.deleteIfExists(file); }
    }
}
