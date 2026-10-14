#!/usr/bin/env bash
# Sign an unsigned Runner.app with a provisioning profile and developer
# certificate that already exist on this Mac. Never talks to the Apple
# Developer portal and never creates credentials: Xcode refuses to pair
# manual signing with an Xcode-managed wildcard profile, and automatic
# signing would register a new App ID online, so we sign with codesign
# directly instead.
#
# Usage: scripts/sign-ios.sh [path/to/Runner.app]
#   JEVONS_IOS_TEAM     team ID whose wildcard profile to use (default SWA3H3N7TW)
#   JEVONS_IOS_PROFILE  profile Name to look for (default "iOS Team Provisioning Profile: *")
set -euo pipefail

APP="${1:-build/ios/iphoneos/Runner.app}"
TEAM="${JEVONS_IOS_TEAM:-SWA3H3N7TW}"
PROFILE_NAME="${JEVONS_IOS_PROFILE:-iOS Team Provisioning Profile: *}"

[[ -d "$APP" ]] || { echo "sign-ios: no app bundle at $APP (run: flutter build ios --release --no-codesign)" >&2; exit 1; }

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# Find the installed profile with the wanted Name and team. Several teams
# publish a profile with the same wildcard name, so Name alone is ambiguous.
profile=""
for dir in "$HOME/Library/Developer/Xcode/UserData/Provisioning Profiles" \
           "$HOME/Library/MobileDevice/Provisioning Profiles"; do
  [[ -d "$dir" ]] || continue
  for f in "$dir"/*.mobileprovision; do
    [[ -f "$f" ]] || continue
    security cms -D -i "$f" > "$tmp/p.plist" 2>/dev/null || continue
    [[ "$(plutil -extract Name raw -o - "$tmp/p.plist")" == "$PROFILE_NAME" ]] || continue
    [[ "$(plutil -extract TeamIdentifier.0 raw -o - "$tmp/p.plist")" == "$TEAM" ]] || continue
    profile="$f"
    break 2
  done
done
[[ -n "$profile" ]] || { echo "sign-ios: no installed profile named '$PROFILE_NAME' for team $TEAM" >&2; exit 1; }

expires="$(plutil -extract ExpirationDate raw -o - "$tmp/p.plist")"
echo "sign-ios: profile $(basename "$profile") (expires $expires)"

# Pick the keychain identity whose certificate the profile embeds.
identity=""
ncerts="$(plutil -extract DeveloperCertificates raw -o - "$tmp/p.plist")"
valid_ids="$(security find-identity -v -p codesigning)"
for ((i = 0; i < ncerts; i++)); do
  sha="$(plutil -extract "DeveloperCertificates.$i" raw -o - "$tmp/p.plist" \
    | base64 -d | openssl x509 -inform DER -noout -fingerprint -sha1 \
    | sed -e 's/^.*=//' -e 's/://g')"
  if grep -q "$sha" <<< "$valid_ids"; then
    identity="$sha"
    break
  fi
done
[[ -n "$identity" ]] || { echo "sign-ios: none of the profile's certificates is a valid signing identity in the keychain" >&2; exit 1; }
echo "sign-ios: identity $(grep "$identity" <<< "$valid_ids" | sed -e 's/^.*"\(.*\)"$/\1/')"

bundle_id="$(plutil -extract CFBundleIdentifier raw -o - "$APP/Info.plist")"
cat > "$tmp/entitlements.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>application-identifier</key>
	<string>$TEAM.$bundle_id</string>
	<key>com.apple.developer.team-identifier</key>
	<string>$TEAM</string>
	<key>get-task-allow</key>
	<true/>
	<key>keychain-access-groups</key>
	<array>
		<string>$TEAM.$bundle_id</string>
	</array>
</dict>
</plist>
EOF

cp "$profile" "$APP/embedded.mobileprovision"

# Nested code first (Flutter engine, Dart AOT snapshot, plugin frameworks),
# then the app itself with its entitlements.
if [[ -d "$APP/Frameworks" ]]; then
  for fw in "$APP/Frameworks"/*.framework "$APP/Frameworks"/*.dylib; do
    [[ -e "$fw" ]] || continue
    codesign --force --sign "$identity" --timestamp=none "$fw"
  done
fi
codesign --force --sign "$identity" --timestamp=none \
  --entitlements "$tmp/entitlements.plist" "$APP"

codesign --verify --deep --strict "$APP"
echo "sign-ios: signed $APP as $TEAM.$bundle_id"
