#!/bin/sh
# Host JNI/sqlpipe persistence smoke, including the real native entry points.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
JDK=${JAVA_HOME:-$(/usr/libexec/java_home -v 21)}
OUT=$(mktemp -d)
trap 'rm -rf "$OUT"' EXIT
cmake -S "$ROOT/android/app/src/main/cpp" -B "$OUT/build" \
  "-DCMAKE_CXX_FLAGS=-I$JDK/include -I$JDK/include/darwin" > "$OUT/configure.log"
cmake --build "$OUT/build" -j 4 > "$OUT/build.log"
"$JDK/bin/javac" -d "$OUT/classes" "$ROOT/android/app/src/test/native/com/canticode/jevons_mobile/widget/DeviceStore.java"
"$JDK/bin/java" -Djava.library.path="$OUT/build" -cp "$OUT/classes" com.canticode.jevons_mobile.widget.DeviceStore
