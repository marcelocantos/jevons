// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import Foundation

/// VoiceLoop binds an AudioEngine to a GrokRealtimeClient: mic frames
/// from the engine flow into Grok; audio deltas from Grok flow into
/// the engine's playback. The mic is always live, server VAD detects
/// utterance boundaries, and AEC + NS + AGC come from the audio
/// unit, so the user can talk freely with no key presses and no
/// echo loop.
public final class VoiceLoop {
    public struct Config {
        public var apiKey: String
        public var voice: String
        public var systemPrompt: String

        public init(apiKey: String,
                    voice: String = "Eve",
                    systemPrompt: String = "") {
            self.apiKey = apiKey
            self.voice = voice
            self.systemPrompt = systemPrompt
        }
    }

    public var onSessionReady: () -> Void = {}
    public var onUserTranscript: (String) -> Void = { _ in }
    public var onAssistantTranscriptDelta: (String) -> Void = { _ in }
    public var onAssistantTranscriptDone: () -> Void = {}
    public var onResponseDone: () -> Void = {}
    public var onError: (Error) -> Void = { _ in }

    private let engine: AudioEngine
    private let grok: GrokRealtimeClient
    private let captureQueue = DispatchQueue(label: "voicelab.capture")

    public init(config: Config) throws {
        self.engine = try AudioEngine()
        let grokConfig = GrokRealtimeClient.Config(
            apiKey: config.apiKey,
            voice: config.voice,
            systemPrompt: config.systemPrompt
        )
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

        // Mic → Grok. The capture callback fires on an audio thread;
        // hop onto a serial queue and a Task wrapper to call the async
        // sendAudio. sendAudio serialises through the WebSocket task
        // internally; we just need to keep it off the audio thread.
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

    public func start() async throws {
        try await grok.connect()
        try engine.start()
    }

    public func stop() {
        engine.stop()
        grok.close()
    }
}
