// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import SwiftUI
import VoicelabKit

/// Throwaway iPad shell for the voice loop. Auto-starts on launch:
///   1. Pull XAI_API_KEY from env (injected by `spyder launch_app
///      env=...` during dev) or from the iOS Keychain.
///   2. Spin up VoiceLoop with server VAD + AVAudioEngine voice
///      processing — same kit the macOS CLI uses.
///   3. Render transcripts as a scrolling list; show a status dot
///      so a wedged session is obvious at a glance.
struct ContentView: View {
    @StateObject private var state = AppState()

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                statusBar
                tuningBar
                Divider()
                transcriptScroll
            }
            .navigationTitle("voicelab")
            .navigationBarTitleDisplayMode(.inline)
        }
        .task { await state.start() }
    }

    /// Live VAD-tuning strip: a dBFS meter with the current trigger
    /// threshold marked, plus a slider to move it. When the green
    /// level bar crosses the threshold tick in .idle, a Grok episode
    /// opens. Remove once the car threshold is dialled in.
    private var tuningBar: some View {
        VStack(spacing: 4) {
            GeometryReader { geo in
                let lo = -80.0, hi = 0.0
                let frac = { (db: Double) in
                    max(0, min(1, (db - lo) / (hi - lo)))
                }
                ZStack(alignment: .leading) {
                    RoundedRectangle(cornerRadius: 3)
                        .fill(.quaternary)
                    RoundedRectangle(cornerRadius: 3)
                        .fill(state.micDB >= state.triggerDB ? Color.green : Color.blue)
                        .frame(width: geo.size.width * frac(state.micDB.isFinite ? state.micDB : lo))
                    Rectangle()
                        .fill(.orange)
                        .frame(width: 2)
                        .offset(x: geo.size.width * frac(state.triggerDB))
                }
            }
            .frame(height: 12)
            HStack {
                Text(state.micDB.isFinite ? String(format: "%.0f dBFS", state.micDB) : "—")
                    .font(.caption.monospaced())
                    .foregroundStyle(.secondary)
                Slider(value: $state.triggerDB, in: -70 ... -20)
                Text(String(format: "trig %.0f", state.triggerDB))
                    .font(.caption.monospaced())
                    .foregroundStyle(.orange)
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 6)
    }

    private var statusBar: some View {
        HStack(spacing: 8) {
            Circle()
                .fill(state.status.color)
                .frame(width: 10, height: 10)
            Text(state.status.label)
                .font(.callout)
                .foregroundStyle(.secondary)
            Spacer()
            if let err = state.lastError {
                Text(err)
                    .font(.caption)
                    .foregroundStyle(.red)
                    .lineLimit(1)
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
    }

    private var transcriptScroll: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 12) {
                    ForEach(state.turns) { turn in
                        TurnView(turn: turn)
                            .id(turn.id)
                    }
                }
                .padding(.horizontal, 16)
                .padding(.vertical, 12)
            }
            .onChange(of: state.turns.last?.id) { _, newID in
                guard let id = newID else { return }
                withAnimation(.easeOut(duration: 0.2)) {
                    proxy.scrollTo(id, anchor: .bottom)
                }
            }
        }
    }
}

private struct TurnView: View {
    let turn: AppState.Turn

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Text(turn.speaker == .user ? "›" : "‹")
                .font(.system(.headline, design: .monospaced))
                .foregroundStyle(turn.speaker == .user ? .blue : .green)
                .frame(width: 16)
            Text(turn.text)
                .font(.body)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}
