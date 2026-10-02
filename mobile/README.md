# jevons-mobile

Cross-platform (iOS + Android) thin WebView shell for the jevons cockpit. Flutter + webview_flutter.
Primary target devices: 'Jevons' iPad mini (A17 Pro, iOS 27.0, connected via Spyder) and a Pixel Fold phone (owner's daily device, connects on-demand for Android testing).

Tracked as 🎯T989 in the jevons ledger (cross-repo, same pattern as 🎯T765.1).

## What it does

- Loads the cockpit URL in a full-screen WebView. The default
  (`https://jevons.canticode.com`) lives in exactly one place:
  `kDefaultCockpitUrl` in `lib/settings.dart`.
- A settings screen (gear icon) changes the URL at runtime. The value is
  persisted with `shared_preferences`, so pointing the shell at a different
  transport (e.g. Pigeon) is a device-side setting, not a rebuild.
- Failed main-frame loads show the error with Retry and Change URL.

Form-factor polish (foldables, hiding the toolbar, etc.) is deferred.

## Platform scaffolding

The `ios/` and `android/` directories are generated, not hand-written.
Run once from the repo root with the Flutter SDK on `PATH`:

```sh
flutter create . --org com.canticode --project-name jevons_mobile --platforms ios,android
flutter pub get
```

This creates bundle ID `com.canticode.jevonsMobile` (iOS) and application ID
`com.canticode.jevons_mobile` (Android). Commit the generated directories.

After generating, add the network permission to
`android/app/src/main/AndroidManifest.xml` (the template only grants it in
the debug and profile manifests):

```xml
<uses-permission android:name="android.permission.INTERNET" />
```

## Build and deploy

```sh
flutter analyze
flutter test
flutter build ios --release      # needs signing configured in Xcode for the bundle ID
flutter build apk --debug        # debug-signed; good enough to verify on a device
```

iOS deploy to the physical iPad goes through Spyder
(`deploy_app device="Jevons"`) with the built `.app` from
`build/ios/iphoneos/Runner.app`. If signing for the new bundle ID is not
already configured, stop and ask the owner; do not set up credentials
autonomously.
