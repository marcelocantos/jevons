# jevons-mobile

Cross-platform (iOS + Android) thin WebView shell for the jevons cockpit. Flutter + webview_flutter.
Primary target devices: 'Jevons' iPad mini (A17 Pro, iOS 27.0, connected via Spyder) and the 'Fold' Pixel 11 Pro Fold (owner's daily device, Spyder alias `Fold`).

Tracked as 🎯T989 in the jevons ledger (cross-repo, same pattern as 🎯T765.1).

## What it does

- Loads the cockpit URL in a full-screen WebView. The default
  (`https://jevons.canticode.com`) lives in exactly one place:
  `kDefaultCockpitUrl` in `lib/settings.dart`.
- No browser chrome: no URL bar, no reload button, no visible settings
  icon. The WebView is the whole screen, so the shell feels like a native
  app rather than a browser.
- No long-press (or other hidden gesture) opens Settings. Owner 2026-10-03:
  settings belong in the cockpit web UI when needed, not as a shell gesture.
- Failed main-frame loads show the error with Retry and Change URL (the
  recovery path into the local Settings screen for the cockpit URL).
  The URL is persisted through sqlpipe on Android and the existing
  `shared_preferences` backend on iOS.

Form-factor polish (foldables etc.) is deferred.

## Platform scaffolding

The `ios/` and `android/` directories were generated with

```sh
flutter create . --org com.canticode --project-name jevons_mobile --platforms ios,android
```

and are committed. Bundle ID `com.canticode.jevonsMobile` (iOS), application
ID `com.canticode.jevons_mobile` (Android). Hand edits on top of the template:
`INTERNET` permission and app label in the main `AndroidManifest.xml`, and the
display name in `ios/Runner/Info.plist`.

Flutter SDK: `brew install --cask flutter`. Android builds use the JDK bundled
with Android Studio (`flutter config --jdk-dir`).

## Build and deploy

```sh
make check   # flutter analyze + flutter test
make ios     # unsigned release build, then scripts/sign-ios.sh
make apk     # release APK signed with the debug keystore
```

### iOS signing

Xcode is deliberately kept out of signing. The team's wildcard development
profile (`iOS Team Provisioning Profile: *`, team SWA3H3N7TW) and its
certificate already exist on the build Mac and already include the Jevons
iPad, but Xcode refuses to use an Xcode-managed profile under manual signing,
and automatic signing would register a new App ID on the developer portal.
`scripts/sign-ios.sh` therefore signs the unsigned `Runner.app` with
`codesign` directly: it embeds the wildcard profile, derives entitlements for
the bundle ID, and uses the keychain identity the profile embeds. No portal
access, no new credentials. `JEVONS_IOS_TEAM` and `JEVONS_IOS_PROFILE`
override the defaults.

### Deploy via Spyder

```python
deploy_app(device="Jevons", owner=..., path=".../build/ios/iphoneos/Runner.app")
deploy_app(device="Pixel", owner=..., bundle_id="com.canticode.jevons_mobile",
           path=".../build/app/outputs/flutter-apk/app-release.apk")
```

Android deploys need `bundle_id` because Spyder derives it with `aapt`, which
is not on `PATH` here. The Pixel Fold is a verified deploy target, not a
deferred one: `deploy_app(device="Fold", …)` with the same `bundle_id`.

## Mobile image attachments (🎯T1020)

The cockpit's attach button asks the Flutter `JevonsImagePicker` JavaScript
channel for a photo/camera image. The shell uses `image_picker` on both
platforms; it sends a JSON-escaped `jevons-picked-image` event containing
base64 bytes, image name/type, and composer ID back to the WebView. The web
composer reconstructs a `File` and uses its existing paste/drag-drop upload
pipeline; browser users use the ordinary hidden file input instead. Cancel
returns no image. Android additionally wires `setOnShowFileSelector` so plain
HTML file inputs can invoke the same native picker, returning a file URI.

Why a JS channel for iOS rather than a custom WKUIDelegate: the latest
`webview_flutter_wkwebview` available to this project (3.27.0) does not expose
`runOpenPanelWithParameters` or any Dart file-selector callback. Replacing the
WebView plugin or swizzling its owned WKUIDelegate would couple the shell to
private implementation and risk breaking navigation/JS delegate behavior. The
supported `webview_flutter` JavaScript channel plus the maintained Flutter
`image_picker` plugin uses the native iOS photo/camera picker without owning
WKUIDelegate at all. `NSCameraUsageDescription` and `NSPhotoLibraryUsageDescription`
are present for native picker permission prompts. Android's legacy HTML input
callback is retained for non-composer pages; the JS channel is the composer
path on both platforms, avoiding file-URI WebView access differences.

## Plan-usage widget (Android, 🎯T1244)

The Android app carries a "Jevons usage" app widget
(`android/app/src/main/kotlin/com/canticode/jevons_mobile/widget/`,
`widgetCategory="keyguard|home_screen"`, API 31+). On the Fold it lives in
the lock screen's widget panel (Settings › Lock screen › Widgets on lock
screen; swipe left from the lock screen, long-press, Add widget).

- **One app, one sign-in.** The worker reads `GET <cockpit URL>/api/plan-usage`
  with the cookies the app's WebView holds for that origin (`CookieManager`),
  i.e. the ngrok Google sign-in. It has no credential, pairing, or endpoint of
  its own. The cockpit URL is the shell's `cockpit_url` in sqlpipe
  (migrated once from `flutter.cockpit_url` in `FlutterSharedPreferences`),
  defaulting to `kDefaultCockpitUrl`.
- **Signed out.** Redirects are not followed: ngrok's 302 to its identity
  provider (or an HTML page) means the session is missing or expired. The
  widget then shows the last reading greyed, with its age, under "Open Jevons
  to sign in".
- **Tap.** A tap anywhere on the widget opens Jevons (from the lock screen,
  after you unlock). Opening or leaving the app refreshes the widget.
- **Look.** One column per backend under its company mark, one bottom-filling
  bar per window coloured by the server's band, a single-letter label (S, W, M,
  or the model's initial), and the cockpit's red border on an exhausted window.
  Bedrock is left out: it is work-only (owner, 2026-10-29).
- **Refresh.** JobScheduler every 15 minutes plus on tap, network-constrained,
  exponential backoff on failure; signed-out is not retried until the app is
  used. Android widget readings and Flutter settings use the embedded sqlpipe database;
  no WorkManager/Room dependency or reflective Room constructor remains.

`make android-test` runs the widget's JVM tests (payload-to-bars mapping,
response classification, signed-out state, RemoteViews layout guard);
`make check` includes it.
