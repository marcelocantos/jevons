// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import Combine
import Foundation
import SwiftUI
import VoicelabKit

/// Observable state for the iPad shell: status indicator, scrolling
/// transcript, last error. Owns the VoiceLoop and survives across
/// the app's lifetime (recreated only on full process restart).
@MainActor
final class AppState: ObservableObject {
    enum Status {
        case idle
        case connecting
        case ready
        case error

        var label: String {
            switch self {
            case .idle: "idle"
            case .connecting: "connecting…"
            case .ready: "ready — talk freely"
            case .error: "error"
            }
        }

        var color: Color {
            switch self {
            case .idle: .secondary
            case .connecting: .yellow
            case .ready: .green
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

    @Published private(set) var status: Status = .idle
    @Published private(set) var turns: [Turn] = []
    @Published private(set) var lastError: String?

    private var loop: VoiceLoop?
    private var streamingTurnIndex: Int?

    func start() async {
        guard loop == nil else { return }
        status = .connecting

        let apiKey: String
        if let envKey = ProcessInfo.processInfo.environment["XAI_API_KEY"],
           !envKey.isEmpty {
            // Persist whatever the launcher injected so subsequent
            // launches don't need the env var (e.g. user double-taps
            // the app after a manual restart).
            do { try Keychain.save(envKey, service: "xai-api-key") } catch {
                // Non-fatal — we still have the key in memory for this session.
            }
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
                systemPrompt: "You are jevons, a voice-first assistant. Keep replies brief and conversational."
            ))
            l.onSessionReady = { [weak self] in
                Task { @MainActor in self?.status = .ready }
            }
            l.onUserSpeechStarted = { [weak self] in
                // Close any in-flight assistant turn. The audio cut-off
                // already happened in VoiceLoop's hook; here we also
                // need the transcript boundary to roll so the next
                // response opens a new bubble instead of appending to
                // the interrupted one.
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
            try await l.start()
        } catch {
            lastError = error.localizedDescription
            status = .error
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
