// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import Foundation

/// VoiceLoop binds an AudioEngine to a GrokRealtimeClient: mic frames
/// from the engine flow into Grok; audio deltas from Grok flow into
/// the engine's playback. Transcripts and lifecycle events surface
/// via the Callbacks the host sets up.
///
/// Two modes:
///   - `continuous`: server VAD detects utterance boundaries. The mic
///     is permanently enabled; barge-in is handled by the
///     speech_started event clearing playback automatically when the
///     host sets `bargeInOnUserStart`.
///   - `pushToTalk`: mic starts disabled; the host calls
///     `startTalking()` and `endTalking()` to gate the mic and trigger
///     a commit. ManualCommit on the Grok side.
public final class VoiceLoop {
    public enum Mode {
        case continuous
        case pushToTalk
    }

    public struct Config {
        public var apiKey: String
        public var voice: String
        public var systemPrompt: String
        public var mode: Mode

        public init(apiKey: String,
                    voice: String = "Eve",
                    systemPrompt: String = "",
                    mode: Mode = .continuous) {
            self.apiKey = apiKey
            self.voice = voice
            self.systemPrompt = systemPrompt
            self.mode = mode
        }
    }

    public var onSessionReady: () -> Void = {}
    public var onUserTranscript: (String) -> Void = { _ in }
    public var onAssistantTranscriptDelta: (String) -> Void = { _ in }
    public var onAssistantTranscriptDone: () -> Void = {}
    public var onResponseDone: () -> Void = {}
    public var onError: (Error) -> Void = { _ in }

    public let mode: Mode

    private let engine: AudioEngine
    private let grok: GrokRealtimeClient
    private let captureQueue = DispatchQueue(label: "voicelab.capture")

    public init(config: Config) throws {
        self.mode = config.mode
        self.engine = try AudioEngine()

        // In continuous mode, mic is on by default. PTT starts muted.
        engine.captureEnabled = (config.mode == .continuous)

        let grokConfig = GrokRealtimeClient.Config(
            apiKey: config.apiKey,
            voice: config.voice,
            systemPrompt: config.systemPrompt,
            manualCommit: config.mode == .pushToTalk
        )

        // Build the Grok client with no callbacks first so `self` is
        // fully initialised; then wire the callbacks. Swift's init
        // analysis requires every stored property be assigned before
        // closures that capture self can be built.
        self.grok = GrokRealtimeClient(config: grokConfig)

        var grokCallbacks = GrokRealtimeClient.Callbacks()
        grokCallbacks.onSessionReady = { [unowned self] in self.onSessionReady() }
        grokCallbacks.onUserTranscript = { [unowned self] text in self.onUserTranscript(text) }
        grokCallbacks.onAssistantTranscriptDelta = { [unowned self] delta in
            self.onAssistantTranscriptDelta(delta)
        }
        grokCallbacks.onAssistantTranscriptDone = { [unowned self] in
            self.onAssistantTranscriptDone()
        }
        grokCallbacks.onAudio = { [unowned self] data in
            self.engine.play(data)
        }
        grokCallbacks.onResponseDone = { [unowned self] in self.onResponseDone() }
        grokCallbacks.onError = { [unowned self] err in self.onError(err) }
        self.grok.setCallbacks(grokCallbacks)

        // Wire mic → Grok. The capture callback fires on an audio
        // thread; we hop onto a serial queue and use a Task wrapper to
        // call the async sendAudio. Concurrency: sendAudio serialises
        // through the WebSocket task internally; we just need to keep
        // it off the audio thread.
        engine.onCapture = { [weak self] data in
            guard let self = self else { return }
            self.captureQueue.async {
                Task { [weak self] in
                    do {
                        try await self?.grok.sendAudio(data)
                    } catch {
                        self?.onError(error)
                    }
                }
            }
        }
    }

    /// Connects to Grok and starts the audio engine. Throws on either
    /// failure; caller is responsible for terminating the process.
    public func start() async throws {
        try await grok.connect()
        try engine.start()
    }

    public func stop() {
        engine.stop()
        grok.close()
    }

    // MARK: - Push-to-talk surface

    /// Unmute the mic. In PTT mode this also clears any audio Grok is
    /// still mid-sentence on (instant barge-in).
    public func startTalking() {
        engine.clearPlayback()
        engine.captureEnabled = true
    }

    /// Mute the mic, then ask Grok to commit + respond.
    public func endTalking() async throws {
        engine.captureEnabled = false
        try await grok.commitAndRespond()
    }
}
