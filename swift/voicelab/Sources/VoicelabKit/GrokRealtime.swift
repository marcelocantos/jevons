// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import Foundation

/// Thin client for xAI Grok Realtime (wss://api.x.ai/v1/realtime).
/// The protocol is OpenAI-Realtime-compatible: JSON event envelopes
/// over WebSocket, audio frames sent and received as base64-encoded
/// PCM16 24 kHz mono.
///
/// Server-side VAD drives utterance boundaries; we don't expose a
/// manual commit path because the OS AEC means the mic can stay
/// live and Grok's own VAD reliably detects end-of-utterance.
public final class GrokRealtimeClient {
    public struct Config {
        public var apiKey: String
        public var voice: String
        public var systemPrompt: String
        /// Logs every incoming event type + key fields to stderr.
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

    public struct Callbacks {
        public var onSessionReady: () -> Void
        /// Server VAD detected the start of a user utterance. Hosts
        /// use this for barge-in: stop the playback of whatever
        /// Grok was mid-sentence on, so the old TTS doesn't overlap
        /// the new exchange.
        public var onUserSpeechStarted: () -> Void
        public var onUserTranscript: (String) -> Void
        public var onAssistantTranscriptDelta: (String) -> Void
        public var onAssistantTranscriptDone: () -> Void
        public var onAudio: (Data) -> Void
        public var onResponseDone: () -> Void
        public var onError: (Error) -> Void

        public init(
            onSessionReady: @escaping () -> Void = {},
            onUserSpeechStarted: @escaping () -> Void = {},
            onUserTranscript: @escaping (String) -> Void = { _ in },
            onAssistantTranscriptDelta: @escaping (String) -> Void = { _ in },
            onAssistantTranscriptDone: @escaping () -> Void = {},
            onAudio: @escaping (Data) -> Void = { _ in },
            onResponseDone: @escaping () -> Void = {},
            onError: @escaping (Error) -> Void = { _ in }
        ) {
            self.onSessionReady = onSessionReady
            self.onUserSpeechStarted = onUserSpeechStarted
            self.onUserTranscript = onUserTranscript
            self.onAssistantTranscriptDelta = onAssistantTranscriptDelta
            self.onAssistantTranscriptDone = onAssistantTranscriptDone
            self.onAudio = onAudio
            self.onResponseDone = onResponseDone
            self.onError = onError
        }
    }

    public enum ClientError: LocalizedError {
        case connectFailed(Error)
        case serverError(String)
        case notConnected
        case malformedEvent(String)

        public var errorDescription: String? {
            switch self {
            case .connectFailed(let err): "grok: connect failed: \(err.localizedDescription)"
            case .serverError(let msg): "grok: server error: \(msg)"
            case .notConnected: "grok: not connected"
            case .malformedEvent(let msg): "grok: malformed event: \(msg)"
            }
        }
    }

    private let config: Config
    private var callbacks: Callbacks
    private var task: URLSessionWebSocketTask?
    private var session: URLSession?
    private var readLoopTask: Task<Void, Never>?
    private var sessionReadyFired = false

    public init(config: Config, callbacks: Callbacks = Callbacks()) {
        self.config = config
        self.callbacks = callbacks
    }

    /// Replace event callbacks after construction. Hosts that want
    /// to capture `self` in callbacks build the client first (so
    /// `self` is fully initialised) then wire the closures.
    public func setCallbacks(_ callbacks: Callbacks) {
        self.callbacks = callbacks
    }

    public func connect() async throws {
        guard let url = URL(string: "wss://api.x.ai/v1/realtime") else {
            throw ClientError.connectFailed(URLError(.badURL))
        }
        var request = URLRequest(url: url)
        request.setValue("Bearer \(config.apiKey)", forHTTPHeaderField: "Authorization")

        let session = URLSession(configuration: .default)
        let task = session.webSocketTask(with: request)
        self.session = session
        self.task = task
        task.resume()

        try await sendSessionUpdate()

        readLoopTask = Task { [weak self] in
            await self?.readLoop()
        }
    }

    public func sendAudio(_ pcm: Data) async throws {
        try await send([
            "type": "input_audio_buffer.append",
            "audio": pcm.base64EncodedString(),
        ])
    }

    public func close() {
        readLoopTask?.cancel()
        readLoopTask = nil
        task?.cancel(with: .normalClosure, reason: nil)
        task = nil
        session?.invalidateAndCancel()
        session = nil
    }

    // MARK: - Private

    private func send(_ payload: [String: Any]) async throws {
        guard let task = task else { throw ClientError.notConnected }
        let data = try JSONSerialization.data(withJSONObject: payload)
        guard let string = String(data: data, encoding: .utf8) else {
            throw ClientError.malformedEvent("encode")
        }
        try await task.send(.string(string))
    }

    private func sendSessionUpdate() async throws {
        var session: [String: Any] = [
            "voice": config.voice,
            "audio": [
                "input": ["format": ["type": "audio/pcm", "rate": 24000]],
                "output": ["format": ["type": "audio/pcm", "rate": 24000]],
            ],
            "turn_detection": [
                "type": "server_vad",
                "threshold": 0.7,
                "silence_duration_ms": 800,
                "prefix_padding_ms": 300,
            ],
        ]
        if !config.systemPrompt.isEmpty {
            session["instructions"] = config.systemPrompt
        }
        try await send(["type": "session.update", "session": session])
    }

    private func readLoop() async {
        guard let task = task else { return }
        while !Task.isCancelled {
            let message: URLSessionWebSocketTask.Message
            do {
                message = try await task.receive()
            } catch {
                if !Task.isCancelled {
                    callbacks.onError(ClientError.connectFailed(error))
                }
                return
            }

            let data: Data
            switch message {
            case .string(let s):
                data = Data(s.utf8)
            case .data(let d):
                data = d
            @unknown default:
                continue
            }

            guard let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
                continue
            }
            handleEvent(json)
        }
    }

    private func handleEvent(_ json: [String: Any]) {
        guard let type = json["type"] as? String else { return }
        if config.verbose {
            FileHandle.standardError.write(Data("grok event: \(type)\n".utf8))
        }
        switch type {
        case "session.created", "conversation.created", "ping":
            break
        case "session.updated":
            if !sessionReadyFired {
                sessionReadyFired = true
                callbacks.onSessionReady()
            }
        case "response.output_audio.delta":
            if let b64 = json["delta"] as? String,
               let bytes = Data(base64Encoded: b64) {
                callbacks.onAudio(bytes)
            }
        case "response.output_audio_transcript.delta":
            if let delta = json["delta"] as? String {
                callbacks.onAssistantTranscriptDelta(delta)
            }
        case "response.output_audio_transcript.done":
            callbacks.onAssistantTranscriptDone()
        case "conversation.item.input_audio_transcription.completed":
            if let text = json["transcript"] as? String {
                callbacks.onUserTranscript(text)
            }
        case "input_audio_buffer.speech_started":
            callbacks.onUserSpeechStarted()
        case "input_audio_buffer.speech_stopped",
             "input_audio_buffer.committed":
            break
        case "response.done":
            callbacks.onResponseDone()
        case "error":
            let msg = (json["error"] as? [String: Any])?["message"] as? String ?? "(unspecified)"
            callbacks.onError(ClientError.serverError(msg))
        default:
            break
        }
    }
}
