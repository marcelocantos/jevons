# Embedded device storage

The native sources are vendored from sqlpipe commit `dda1ae8e52824b61da2b3fd16224748d212d2aa0`:
`swift/Sources/CSqlpipe/{sqlpipe.cpp,sqlpipe.h,sqlite3.c,lz4.c,include/{sqlite3.h,lz4.h,nlohmann/json.hpp},deepparser/*}`.
The sqlpipe distribution is Apache-2.0; SQLite is public domain, LZ4 is BSD-2-Clause,
nlohmann/json is MIT, and deepparser is Apache-2.0. SQLite is bundled rather than
using Android's framework SQLite: sqlpipe requires the session and preupdate hooks.
The database is local only, with no replication endpoint or credential.

`DeviceStore` opens sqlpipe's `Database` with a one-table schema. The JNI bridge
serialises access and binds UTF-8 values as hex-encoded SQL literals; widget
payload and timestamp are written in one transaction. The Flutter platform
channel and the native widget share that database. Old SharedPreferences values
are migrated once on first access, then removed after successful sqlpipe write.

To verify an Android build: from `mobile`, run `make android-test`, `make apk`,
`cd android && ./gradlew :app:dependencies --configuration releaseRuntimeClasspath`
and inspect the release APK dex for `androidx/room/` (neither Gradle dependencies
nor APK dex should contain any). Install the release APK and launch on a test
Android device, then place and tap the widget to verify refresh.
