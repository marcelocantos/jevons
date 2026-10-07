FLUTTER ?= flutter
IOS_APP := build/ios/iphoneos/Runner.app
APK := build/app/outputs/flutter-apk/app-release.apk

.PHONY: all check analyze test ios apk clean

all: check ios apk

check: analyze test

analyze:
	$(FLUTTER) analyze

test:
	$(FLUTTER) test

# Unsigned Xcode build, then local codesign with the existing wildcard
# development profile. See scripts/sign-ios.sh for why Xcode is not
# allowed to sign this target itself.
ios:
	$(FLUTTER) build ios --release --no-codesign
	scripts/sign-ios.sh $(IOS_APP)

# Release build signed with the Android debug keystore (the template
# default); good enough for on-device verification.
apk:
	$(FLUTTER) build apk --release

clean:
	$(FLUTTER) clean
