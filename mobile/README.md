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
  The URL is still persisted with `shared_preferences` for transport swaps.

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
