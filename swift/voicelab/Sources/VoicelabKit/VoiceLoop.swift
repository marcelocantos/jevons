// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import Foundation

/// VoiceLoop binds the always-on AudioEngine to a per-episode
/// GrokRealtimeClient. The shape is:
///
///   idle      → local VAD scans the AEC-cleaned mic. Pre-roll ring
///               keeps the last ~500 ms so we don't clip the
///               trigger utterance. Audio engine runs, Grok WS is
///               closed (and not billing).
///
///   active    → a Grok session is open; full-duplex. Server VAD
///               drives turn boundaries. The user can barge in;
///               we cut local playback on speech_started.
///
///   closing   → response complete, a silence timer is running. If
///               the user starts a new utterance before the timer
///               fires (server VAD speech_started), we cancel the
///               timer and return to active. Otherwise we close the
///               WS and return to idle.
///
/// Conversation continuity does not live in Grok — each session
/// starts cold. State that survives episodes lives in the host
/// (UI transcript log, jevonsd backend, …).
public final class VoiceLoop {
    public enum SessionState: String {
        case idle
        case active
        case closing
    }

    public struct Config {
        public var apiKey: String
        public var voice: String
        public var systemPrompt: String
        public var vad: LocalVAD
        public var preRollMs: Int
        /// How long to wait after response.done before closing the
        /// Grok session. If the user starts a new utterance before
        /// this fires, the timer is cancelled and we stay active.
        public var postResponseSilenceMs: Int

        public init(apiKey: String,
                    voice: String = "Eve",
                    systemPrompt: String = "",
                    vad: LocalVAD = LocalVAD(),
                    preRollMs: Int = 500,
                    postResponseSilenceMs: Int = 10_000) {
            self.apiKey = apiKey
            self.voice = voice
            self.systemPrompt = systemPrompt
            self.vad = vad
            self.preRollMs = preRollMs
            self.postResponseSilenceMs = postResponseSilenceMs
        }
    }

    public var onSessionReady: () -> Void = {}
    public var onUserSpeechStarted: () -> Void = {}
    public var onUserTranscript: (String) -> Void = { _ in }
    public var onAssistantTranscriptDelta: (String) -> Void = { _ in }
    public var onAssistantTranscriptDone: () -> Void = {}
    public var onResponseDone: () -> Void = {}
    public var onError: (Error) -> Void = { _ in }
    public var onStateChange: (SessionState) -> Void = { _ in }
    /// Fires ~10×/s with the current mic level in dBFS (AEC-cleaned
    /// signal). For on-device VAD tuning — without terminal stderr on
    /// a GUI app, a live meter is the only way to pick `triggerDB`.
    public var onMicLevel: (Double) -> Void = { _ in }

    public private(set) var state: SessionState = .idle {
        didSet {
            if state != oldValue { onStateChange(state) }
        }
    }

    private let config: Config
    private let engine: AudioEngine
    private let sessionQueue = DispatchQueue(label: "voicelab.session")
    private var grok: GrokRealtimeClient?
    private var vad: LocalVAD
    private var preRoll: PreRollBuffer
    private var silenceTask: Task<Void, Never>?
    private let stateLock = NSLock()
    private var levelAccumMs: Double = 0

    public init(config: Config) throws {
        self.config = config
        self.engine = try AudioEngine()
        self.vad = config.vad
        self.preRoll = PreRollBuffer(capacityMs: config.preRollMs)

        engine.onCapture = { [weak self] data in
            self?.handleCapture(data)
        }
    }

    public func start() throws {
        try engine.start()
    }

    public func stop() {
        silenceTask?.cancel()
        grok?.close()
        grok = nil
        engine.stop()
        state = .idle
    }

    /// Retune the local VAD trigger threshold while running. Used by
    /// the on-device tuning UI.
    public func setTriggerDB(_ db: Double) {
        stateLock.lock()
        vad.triggerDB = db
        stateLock.unlock()
    }

    // MARK: - Capture pipeline

    private func handleCapture(_ pcm: Data) {
        // Always feed the pre-roll so the trigger utterance survives.
        // PreRoll only matters in .idle but keeping it always-current
        // is cheaper than gating + flushing on state transitions.
        stateLock.lock()
        preRoll.append(pcm)
        let currentState = state
        stateLock.unlock()

        reportLevel(pcm)

        switch currentState {
        case .idle:
            stateLock.lock()
            let triggered = vad.ingest(pcm, sampleRate: AudioEngine.sampleRate)
            stateLock.unlock()
            if triggered {
                openSession()
            }

        case .active, .closing:
            // Stream live mic into the open session. Grok's server
            // VAD handles utterance boundaries within the episode.
            if let g = grok {
                Task {
                    do { try await g.sendAudio(pcm) } catch {
                        self.onError(error)
                    }
                }
            }
        }
    }

    /// Throttled dBFS meter for the UI. ~10 reports/sec is smooth
    /// enough to read and cheap enough to ignore.
    private func reportLevel(_ pcm: Data) {
        let samples = pcm.count / MemoryLayout<Int16>.size
        guard samples > 0 else { return }
        levelAccumMs += Double(samples) / AudioEngine.sampleRate * 1000
        guard levelAccumMs >= 100 else { return }
        levelAccumMs = 0
        var sumSquares: Double = 0
        pcm.withUnsafeBytes { raw in
            for v in raw.bindMemory(to: Int16.self) {
                let f = Double(v)
                sumSquares += f * f
            }
        }
        let rms = (sumSquares / Double(samples)).squareRoot()
        let dB = rms > 0 ? 20 * log10(rms / 32767.0) : -Double.infinity
        onMicLevel(dB)
    }

    // MARK: - Session lifecycle

    private func openSession() {
        sessionQueue.async { [weak self] in
            guard let self = self else { return }
            self.stateLock.lock()
            guard self.state == .idle else {
                self.stateLock.unlock()
                return
            }
            // Transitional — we don't expose .opening; the next state
            // change observable from outside is .active.
            self.stateLock.unlock()

            let g = GrokRealtimeClient(config: .init(
                apiKey: self.config.apiKey,
                voice: self.config.voice,
                systemPrompt: self.config.systemPrompt
            ))

            var cb = GrokRealtimeClient.Callbacks()
            cb.onSessionReady = { [weak self] in
                guard let self = self else { return }
                self.stateLock.lock()
                let pre = self.preRoll.drain()
                self.stateLock.unlock()
                // Flush the pre-roll so Grok hears the leading audio
                // that fired our local VAD. Without this, the first
                // syllable or two land outside the session window and
                // get chopped from the transcript.
                Task { [weak self] in
                    if !pre.isEmpty {
                        do { try await g.sendAudio(pre) } catch {
                            self?.onError(error)
                        }
                    }
                    await MainActor.run {
                        self?.state = .active
                        self?.onSessionReady()
                    }
                }
            }
            cb.onUserSpeechStarted = { [weak self] in
                guard let self = self else { return }
                self.cancelSilenceTimer()
                self.engine.stopPlayback()
                self.state = .active
                self.onUserSpeechStarted()
            }
            cb.onUserTranscript = { [weak self] t in self?.onUserTranscript(t) }
            cb.onAssistantTranscriptDelta = { [weak self] d in self?.onAssistantTranscriptDelta(d) }
            cb.onAssistantTranscriptDone = { [weak self] in self?.onAssistantTranscriptDone() }
            cb.onAudio = { [weak self] data in self?.engine.play(data) }
            cb.onResponseDone = { [weak self] in
                self?.onResponseDone()
                self?.startSilenceTimer()
            }
            cb.onError = { [weak self] err in self?.onError(err) }
            g.setCallbacks(cb)

            self.grok = g

            Task { [weak self] in
                do {
                    try await g.connect()
                } catch {
                    self?.onError(error)
                    await MainActor.run { self?.state = .idle }
                    self?.grok = nil
                }
            }
        }
    }

    private func startSilenceTimer() {
        cancelSilenceTimer()
        state = .closing
        let timeout = TimeInterval(config.postResponseSilenceMs) / 1000.0
        silenceTask = Task { [weak self] in
            try? await Task.sleep(nanoseconds: UInt64(timeout * 1_000_000_000))
            guard !Task.isCancelled, let self = self else { return }
            await MainActor.run {
                guard self.state == .closing else { return }
                self.closeSession()
            }
        }
    }

    private func cancelSilenceTimer() {
        silenceTask?.cancel()
        silenceTask = nil
    }

    private func closeSession() {
        grok?.close()
        grok = nil
        state = .idle
    }
}
