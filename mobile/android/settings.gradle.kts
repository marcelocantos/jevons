pluginManagement {
    val flutterSdkPath =
        run {
            val properties = java.util.Properties()
            val local = file("local.properties")
            if (local.isFile) local.inputStream().use { properties.load(it) }
            val flutterSdkPath = properties.getProperty("flutter.sdk") ?: System.getenv("FLUTTER_ROOT")
            require(!flutterSdkPath.isNullOrBlank()) {
                "Flutter SDK not set: supply local.properties flutter.sdk or FLUTTER_ROOT"
            }
            flutterSdkPath
        }

    includeBuild("$flutterSdkPath/packages/flutter_tools/gradle")

    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}

plugins {
    id("dev.flutter.flutter-plugin-loader") version "1.0.0"
    id("com.android.application") version "9.1.0" apply false
    id("org.jetbrains.kotlin.android") version "2.4.0" apply false
}

include(":app")
