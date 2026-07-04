BUILD_DIR := build
CXX       := clang++

-include ge/Module.mk
ge/Module.mk:
	git submodule update --init --recursive

# ── Flags ────────────────────────────────────────────
CXXFLAGS   := -std=c++20 -O2 -Wall $(ge/INCLUDES)
SDL_CFLAGS :=
SDL_LIBS   := $(ge/SDL_LIBS)
FRAMEWORKS := -framework Metal -framework QuartzCore -framework Foundation \
              -framework CoreFoundation -framework IOKit -framework IOSurface \
              -framework CoreGraphics -framework CoreServices \
              -framework AudioToolbox -framework AVFoundation -framework CoreMedia \
              -framework CoreVideo -framework GameController -framework CoreHaptics \
              -framework CoreMotion -framework ImageIO

# ── C++ app ──────────────────────────────────────────
SRC := src/main.cpp src/App.cpp
OBJ := $(patsubst %.cpp,$(BUILD_DIR)/%.o,$(SRC))
APP := bin/jevons

COMPILE_DB_DEPS += $(SRC) Makefile

# ── Default target ───────────────────────────────────
.PHONY: all
all: $(APP) jevonsd remote

# ── C++ binary ───────────────────────────────────────
$(APP): $(OBJ) $(ge/SESSION_WIRE_OBJ) $(ge/LIB) $(ge/FRAMEWORK_LIBS)
	@mkdir -p $(@D)
	$(CXX) $(OBJ) $(ge/SESSION_WIRE_OBJ) $(ge/LIB) $(ge/DAWN_LIBS) \
		$(FRAMEWORKS) $(SDL_LIBS) -o $@

$(BUILD_DIR)/src/%.o: src/%.cpp
	@mkdir -p $(dir $@)
	$(CXX) $(CXXFLAGS) $(SDL_CFLAGS) -MMD -MP -c $< -o $@

-include $(OBJ:.o=.d)

# ── Player ───────────────────────────────────────────
.PHONY: player
player: $(ge/PLAYER)

# ── Go binaries ─────────────────────────────────────
VERSION  ?= dev
LDFLAGS  := -ldflags "-X github.com/marcelocantos/jevons/internal/cli.Version=$(VERSION)"
GO_SRC   := $(shell find cmd internal -name '*.go' 2>/dev/null)
EMBED_GUIDE := internal/cli/help_agent.md

$(EMBED_GUIDE): agents-guide.md
	cp $< $@

.PHONY: jevonsd
jevonsd: bin/jevonsd

bin/jevonsd: $(GO_SRC) $(EMBED_GUIDE)
	@mkdir -p bin
	go build $(LDFLAGS) -o bin/jevonsd ./cmd/jevonsd

.PHONY: remote
remote: bin/remote

bin/remote: $(GO_SRC) $(EMBED_GUIDE)
	@mkdir -p bin
	go build $(LDFLAGS) -o bin/remote ./cmd/remote

# ── voicelab (Swift CLI — AVAudioEngine + Grok Realtime) ─────
VOICELAB_SRC := $(shell find swift/voicelab/Sources -name '*.swift' 2>/dev/null) swift/voicelab/Package.swift

.PHONY: voicelab run-voicelab
voicelab: bin/voicelab

bin/voicelab: $(VOICELAB_SRC)
	@mkdir -p bin
	cd swift/voicelab && swift build -c release
	cp swift/voicelab/.build/release/voicelab bin/voicelab

run-voicelab: bin/voicelab
	bin/voicelab

# ── voicelab iPad app ───────────────────────────────
# JEVONS_DEVICE_ID: the iPad's devicectl identifier (Jevons mini).
# -allowProvisioningDeviceRegistration self-heals the free-provisioning
# weekly device de-registration that otherwise fails the build with
# "your team has no devices". After a cert regen the app must be
# re-trusted once on-device (Settings → General → VPN & Device Mgmt).
JEVONS_DEVICE_ID ?= 53F85BAC-9B4A-5392-886A-DE30C2F764E3
VOICELAB_APP := swift/voicelab/ios/build/Build/Products/Release-iphoneos/VoicelabApp.app

.PHONY: voicelab-ios voicelab-deploy
voicelab-ios:
	cd swift/voicelab/ios && xcodegen generate
	cd swift/voicelab/ios && xcodebuild -project VoicelabApp.xcodeproj -scheme VoicelabApp \
		-configuration Release -destination 'id=$(JEVONS_DEVICE_ID)' -derivedDataPath build \
		-allowProvisioningUpdates -allowProvisioningDeviceRegistration build

voicelab-deploy: voicelab-ios
	xcrun devicectl device install app --device $(JEVONS_DEVICE_ID) $(VOICELAB_APP)
	@KEY=$$(security find-generic-password -a jevons -s xai-api-key -w 2>/dev/null | tr -d '\n'); \
	xcrun devicectl device process launch --device $(JEVONS_DEVICE_ID) \
		--environment-variables "{\"XAI_API_KEY\":\"$$KEY\"}" com.marcelocantos.voicelab

# ── Run ──────────────────────────────────────────────
.PHONY: run run-app run-jevonsd run-remote
run-app: $(APP)
	$(APP)

run-jevonsd: bin/jevonsd
	bin/jevonsd

run-remote: bin/remote
	bin/remote

run: $(APP) bin/jevonsd
	@trap 'kill 0' INT TERM; \
	bin/jevonsd & \
	$(APP) & \
	wait

# ── Setup ────────────────────────────────────────────
.PHONY: init
init: ge/init
	@echo "── jevons project setup ──"
	@command -v go >/dev/null 2>&1 || { echo "ERROR: Go not found. Install from https://go.dev/dl/"; exit 1; }
	@echo "  Go: $$(go version)"
	@go mod download
	@echo "  Go dependencies downloaded"
	$(ge/INIT_DONE)

# ── iOS app ─────────────────────────────────────────
.PHONY: ios
ios:
	cd ios && xcodegen generate

# ── Test ─────────────────────────────────────────────
.PHONY: test test-go
test-go:
	go test ./...

test: test-go

# ── Standing invariants (bullseye) ──────────────────
.PHONY: bullseye
bullseye:
	@go build ./... && echo "✓ build"
	@go test ./... && echo "✓ tests"
	@go vet ./... && echo "✓ vet"
	@test -z "$$(git status --porcelain)" && echo "✓ clean" || \
	 (echo "✗ dirty tree"; git status --short; exit 1)
