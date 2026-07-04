// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import Combine
import Foundation
import SwiftUI
import VoicelabKit

/// Observable state for the iPad shell: status indicator, scrolling
/// transcript, last error. Owns the VoiceLoop (always-on locally,
/// VAD-gated Grok sessions on top) and survives across the app's
/// lifetime.
@MainActor
final class AppState: ObservableObject {
    enum Status {
        case bootstrapping
        case idle
        case active
        case closing
        case error

        var label: String {
            switch self {
            case .bootstrapping: "starting…"
            case .idle: "listening — talk to wake"
            case .active: "live — talk freely"
            case .closing: "wrapping up…"
            case .error: "error"
            }
        }

        var color: Color {
            switch self {
            case .bootstrapping: .yellow
            case .idle: .secondary
            case .active: .green
            case .closing: .orange
            case .error: .red
            }
        }
    }

    struct Turn: Identifiable {
        enum Speaker { case user, jevons, worker }
        let id = UUID()
        let speaker: Speaker
        var text: String
    }

    @Published private(set) var status: Status = .bootstrapping
    @Published private(set) var turns: [Turn] = []
    @Published private(set) var lastError: String?
    /// Live mic level in dBFS for the tuning meter.
    @Published private(set) var micDB: Double = -Double.infinity
    /// VAD trigger threshold, adjustable from the UI while tuning.
    @Published var triggerDB: Double = -38 {
        didSet { loop?.setTriggerDB(triggerDB) }
    }

    /// Diagnostic counters (bridge bring-up): user transcripts,
    /// overseer replies, worker notes, and the last reply text seen.
    @Published private(set) var debugLine: String = ""
    private var nUser = 0, nReply = 0, nWorker = 0

    /// True when routing through the Claude overseer (JEVONS_OVERSEER_URL set).
    private(set) var bridge = false
    /// Overseer link connectivity (bridge mode only).
    @Published private(set) var overseerConnected = false

    private var loop: VoiceLoop?
    private var streamingTurnIndex: Int?

    func start() async {
        guard loop == nil else { return }

        let apiKey: String
        if let envKey = ProcessInfo.processInfo.environment["XAI_API_KEY"],
           !envKey.isEmpty {
            do { try Keychain.save(envKey, service: "xai-api-key") } catch {}
            apiKey = envKey
        } else {
            do {
                apiKey = try Keychain.lookup(service: "xai-api-key")
            } catch {
                lastError = error.localizedDescription
                status = .error
                return
            }
        }

        // Bridge mode: JEVONS_OVERSEER_URL (e.g. ws://192.168.1.217:13705)
        // routes transcripts to the Claude overseer over jevonsd /ws/chat.
        // Absent → standalone Grok conversational mode.
        let env = ProcessInfo.processInfo.environment
        let overseerURL = (env["JEVONS_OVERSEER_URL"]).flatMap(URL.init(string:))
        self.bridge = overseerURL != nil

        do {
            let l = try VoiceLoop(config: .init(
                apiKey: apiKey,
                voice: "Eve",
                systemPrompt: "You are jevons, a voice-first assistant. Keep replies brief and conversational.",
                vad: LocalVAD(triggerDB: triggerDB),
                overseerURL: overseerURL
            ))
            l.onStateChange = { [weak self] s in
                Task { @MainActor in self?.applyState(s) }
            }
            l.onMicLevel = { [weak self] db in
                Task { @MainActor in self?.micDB = db }
            }
            l.onUserSpeechStarted = { [weak self] in
                Task { @MainActor in self?.completeAssistantTurn() }
            }
            l.onUserTranscript = { [weak self] text in
                Task { @MainActor in
                    self?.nUser += 1
                    self?.appendUserTurn(text)
                    self?.refreshDebug()
                }
            }
            l.onError = { [weak self] err in
                Task { @MainActor in
                    self?.lastError = err.localizedDescription
                    self?.status = .error
                }
            }

            if self.bridge {
                // Assistant turns come from the overseer's reply text
                // (authoritative); Grok's TTS-echo transcript is ignored
                // to avoid duplication.
                l.onOverseerReply = { [weak self] text in
                    Task { @MainActor in
                        self?.nReply += 1
                        self?.appendAssistantDelta(text)
                        self?.refreshDebug(lastReply: text)
                    }
                }
                l.onResponseDone = { [weak self] in
                    Task { @MainActor in self?.completeAssistantTurn() }
                }
                l.onWorkerNote = { [weak self] note in
                    Task { @MainActor in
                        self?.nWorker += 1
                        self?.appendWorkerNote(note)
                        self?.refreshDebug()
                    }
                }
                l.onOverseerConnected = { [weak self] in
                    Task { @MainActor in self?.overseerConnected = true }
                }
                l.onOverseerDisconnected = { [weak self] _ in
                    Task { @MainActor in self?.overseerConnected = false }
                }
            } else {
                l.onAssistantTranscriptDelta = { [weak self] delta in
                    Task { @MainActor in self?.appendAssistantDelta(delta) }
                }
                l.onAssistantTranscriptDone = { [weak self] in
                    Task { @MainActor in self?.completeAssistantTurn() }
                }
            }

            self.loop = l
            try l.start()
            status = .idle
        } catch {
            lastError = error.localizedDescription
            status = .error
        }
    }

    private func applyState(_ s: VoiceLoop.SessionState) {
        switch s {
        case .idle:    status = .idle
        case .active:  status = .active
        case .closing: status = .closing
        }
    }

    private func appendUserTurn(_ text: String) {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        turns.append(.init(speaker: .user, text: trimmed))
    }

    private func appendAssistantDelta(_ delta: String) {
        if let idx = streamingTurnIndex, idx < turns.count {
            turns[idx].text += delta
        } else {
            turns.append(.init(speaker: .jevons, text: delta))
            streamingTurnIndex = turns.count - 1
        }
    }

    private func completeAssistantTurn() {
        streamingTurnIndex = nil
    }

    private var lastReplyText = ""
    private func refreshDebug(lastReply: String? = nil) {
        if let r = lastReply { lastReplyText = r }
        let tail = lastReplyText.isEmpty ? "" : " · «\(lastReplyText.prefix(40))»"
        debugLine = "u:\(nUser) r:\(nReply) w:\(nWorker)\(tail)"
    }

    private func appendWorkerNote(_ note: OverseerLink.WorkerNote) {
        // Close any in-flight assistant stream so the note stands alone.
        streamingTurnIndex = nil
        let tag = note.failed ? "⚠︎" : "✓"
        let head = note.agent.isEmpty ? "" : "\(note.agent): "
        turns.append(.init(speaker: .worker, text: "\(tag) \(head)\(note.content)"))
    }
}
