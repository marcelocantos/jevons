// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import Foundation

/// OverseerLink is the persistent control channel between the iPad
/// voice client and the jevons Claude overseer running under jevonsd.
/// It speaks jevonsd's `/ws/chat` protocol:
///
///   client → server : a plain-text string is one chat turn to the
///                      overseer. ("stop" interrupts; {"type":"ping"}
///                      is a heartbeat.)
///   server → client : raw Claude Code stream-json JSONL lines — the
///                      overseer's live output — plus `worker_note`
///                      frames for async agent/worker completions.
///
/// We forward the user's final Grok transcript as a turn, extract the
/// overseer's assistant *text* (ignoring tool_use / tool_result
/// plumbing) to voice back through the Grok episode, and surface
/// worker_note frames as async "while you were away" items.
///
/// No audio ever crosses this link — only text. On the car iPad it
/// carries a few hundred bytes per turn, so it is fine over cellular
/// or a home tunnel; voice audio stays on the direct iPad↔Grok path.
public final class OverseerLink {
    public struct Callbacks {
        /// Fires with each assistant text block as it streams in.
        public var onReplyText: (String) -> Void
        /// Fires when the overseer's turn completes (stream-json
        /// `result` event) — the cue to close out TTS for this turn.
        public var onTurnComplete: () -> Void
        /// Async agent/worker completion (a `worker_note` frame).
        public var onWorkerNote: (WorkerNote) -> Void
        public var onConnected: () -> Void
        public var onDisconnected: (Error?) -> Void
        public var onError: (Error) -> Void

        public init(
            onReplyText: @escaping (String) -> Void = { _ in },
            onTurnComplete: @escaping () -> Void = {},
            onWorkerNote: @escaping (WorkerNote) -> Void = { _ in },
            onConnected: @escaping () -> Void = {},
            onDisconnected: @escaping (Error?) -> Void = { _ in },
            onError: @escaping (Error) -> Void = { _ in }
        ) {
            self.onReplyText = onReplyText
            self.onTurnComplete = onTurnComplete
            self.onWorkerNote = onWorkerNote
            self.onConnected = onConnected
            self.onDisconnected = onDisconnected
            self.onError = onError
        }
    }

    public struct WorkerNote {
        public let agent: String
        public let taskID: String
        public let content: String
        public let failed: Bool
    }

    /// Base URL of jevonsd, e.g. ws://192.168.1.217:13705 (desk LAN)
    /// or wss://<tunnel> (car). `/ws/chat` is appended.
    private let baseURL: URL
    private var callbacks: Callbacks
    private var task: URLSessionWebSocketTask?
    private var session: URLSession?
    private var readLoopTask: Task<Void, Never>?
    /// True only between sending a turn and that turn completing. Gates
    /// out the JSONL history burst jevonsd replays on connect (which
    /// arrives before any turn is sent) and any stray assistant events
    /// between turns — we voice a reply only when we asked for one.
    private var expectingReply = false

    public init(baseURL: URL, callbacks: Callbacks = Callbacks()) {
        self.baseURL = baseURL
        self.callbacks = callbacks
    }

    public func connect() {
        let url = baseURL.appendingPathComponent("ws/chat")
        let session = URLSession(configuration: .default)
        let task = session.webSocketTask(with: url)
        self.session = session
        self.task = task
        task.resume()
        callbacks.onConnected()
        readLoopTask = Task { [weak self] in await self?.readLoop() }
    }

    /// Send the user's transcribed turn to the overseer.
    public func sendTurn(_ text: String) {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty, let task = task else { return }
        expectingReply = true
        task.send(.string(trimmed)) { [weak self] err in
            if let err = err { self?.callbacks.onError(err) }
        }
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

    private func readLoop() async {
        guard let task = task else { return }
        while !Task.isCancelled {
            let message: URLSessionWebSocketTask.Message
            do {
                message = try await task.receive()
            } catch {
                if !Task.isCancelled { callbacks.onDisconnected(error) }
                return
            }
            let text: String
            switch message {
            case .string(let s): text = s
            case .data(let d): text = String(decoding: d, as: UTF8.self)
            @unknown default: continue
            }
            handleLine(text)
        }
    }

    private func handleLine(_ line: String) {
        guard let data = line.data(using: .utf8),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let type = obj["type"] as? String
        else { return }

        switch type {
        case "assistant":
            // Only voice replies to a turn we actually sent — ignores
            // the connect-time history burst and inter-turn stragglers.
            guard expectingReply else { return }
            let message = obj["message"] as? [String: Any] ?? [:]
            // Voice the text blocks; ignore tool_use (delegation is
            // silent). Intermediate "let me check…" messages before a
            // tool call are worth voicing — they fill Claude's latency.
            let content = message["content"] as? [[String: Any]] ?? []
            for c in content where (c["type"] as? String) == "text" {
                if let t = c["text"] as? String, !t.isEmpty {
                    callbacks.onReplyText(t)
                }
            }
            // The turn's final message carries stop_reason "end_turn"
            // (intermediate tool-call messages are "tool_use"). The
            // Claude Code PTY stream has no separate "result" event.
            if (message["stop_reason"] as? String) == "end_turn" {
                expectingReply = false
                callbacks.onTurnComplete()
            }
        case "worker_note":
            // Async agent/worker completion — surfaces regardless of
            // history grace (a completion is always "new").
            let note = WorkerNote(
                agent: obj["agent"] as? String ?? "worker",
                taskID: obj["task_id"] as? String ?? "",
                content: obj["content"] as? String ?? "",
                failed: (obj["failed"] as? Bool) ?? false
            )
            callbacks.onWorkerNote(note)
        default:
            break
        }
    }
}
