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
        enum Speaker { case user, jevons }
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

        do {
            let l = try VoiceLoop(config: .init(
                apiKey: apiKey,
                voice: "Eve",
                systemPrompt: "You are jevons, a voice-first assistant. Keep replies brief and conversational.",
                vad: LocalVAD(triggerDB: triggerDB)
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
                Task { @MainActor in self?.appendUserTurn(text) }
            }
            l.onAssistantTranscriptDelta = { [weak self] delta in
                Task { @MainActor in self?.appendAssistantDelta(delta) }
            }
            l.onAssistantTranscriptDone = { [weak self] in
                Task { @MainActor in self?.completeAssistantTurn() }
            }
            l.onError = { [weak self] err in
                Task { @MainActor in
                    self?.lastError = err.localizedDescription
                    self?.status = .error
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
}
