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
        /// Forwards to GrokRealtimeClient.Config.verbose and adds
        /// periodic mic RMS lines so a silent mic is obvious.
        public var verbose: Bool

        public init(apiKey: String,
                    voice: String = "Eve",
                    systemPrompt: String = "",
                    verbose: Bool = false) {
            self.apiKey = apiKey
            self.voice = voice
            self.systemPrompt = systemPrompt
            self.verbose = verbose
        }
    }

    public var onSessionReady: () -> Void = {}
    public var onUserSpeechStarted: () -> Void = {}
    public var onUserTranscript: (String) -> Void = { _ in }
    public var onAssistantTranscriptDelta: (String) -> Void = { _ in }
    public var onAssistantTranscriptDone: () -> Void = {}
    public var onResponseDone: () -> Void = {}
    public var onError: (Error) -> Void = { _ in }

    private let engine: AudioEngine
    private let grok: GrokRealtimeClient
    private let captureQueue = DispatchQueue(label: "voicelab.capture")
    private let verbose: Bool
    private var lastMicLogAt = Date.distantPast
    private var micFrames = 0
    private var micSumSquares: Double = 0
    private let micLogLock = NSLock()

    public init(config: Config) throws {
        self.verbose = config.verbose
        self.engine = try AudioEngine()
        let grokConfig = GrokRealtimeClient.Config(
            apiKey: config.apiKey,
            voice: config.voice,
            systemPrompt: config.systemPrompt,
            verbose: config.verbose
        )
        self.grok = GrokRealtimeClient(config: grokConfig)

        var grokCallbacks = GrokRealtimeClient.Callbacks()
        grokCallbacks.onSessionReady = { [unowned self] in self.onSessionReady() }
        grokCallbacks.onUserSpeechStarted = { [unowned self] in
            // Cut off whatever Grok was still mid-saying. Server-side
            // Grok already abandons its in-flight response when VAD
            // fires speech_started; this catches up the local queue.
            self.engine.stopPlayback()
            self.onUserSpeechStarted()
        }
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
            if self.verbose { self.accumulateMicRMS(data) }
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

    /// Periodically log mic RMS in dBFS so a silent or stuck mic is
    /// obvious. Logs at most once per second; if the mic is dead the
    /// dBFS line stops appearing entirely.
    private func accumulateMicRMS(_ pcm: Data) {
        var sum: Double = 0
        var count = 0
        pcm.withUnsafeBytes { raw in
            let p = raw.bindMemory(to: Int16.self)
            for v in p {
                let f = Double(v)
                sum += f * f
                count += 1
            }
        }
        micLogLock.lock()
        defer { micLogLock.unlock() }
        micFrames += count
        micSumSquares += sum
        let now = Date()
        if now.timeIntervalSince(lastMicLogAt) >= 1.0, micFrames > 0 {
            let rms = (micSumSquares / Double(micFrames)).squareRoot()
            let dB = rms > 0 ? 20 * log10(rms / 32767.0) : -.infinity
            let msg = String(format: "mic: %.1f dBFS (%d frames)\n", dB, micFrames)
            FileHandle.standardError.write(Data(msg.utf8))
            lastMicLogAt = now
            micFrames = 0
            micSumSquares = 0
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
