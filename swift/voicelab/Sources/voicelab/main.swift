// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import Darwin
import Foundation
import VoicelabKit

// Argument parsing — tiny rolled-by-hand parser; ArgumentParser would
// be lovely but adding a SwiftPM dep for three flags isn't worth it.
struct CLIArgs {
    var system: String = "You are jevons, a voice-first assistant. Keep replies brief and conversational."
    var voice: String = "Eve"
    var continuous: Bool = false
    var verbose: Bool = false
}

func parseArgs() -> CLIArgs {
    var args = CLIArgs()
    var iter = CommandLine.arguments.dropFirst().makeIterator()
    while let arg = iter.next() {
        switch arg {
        case "--continuous":
            args.continuous = true
        case "-v", "--verbose":
            args.verbose = true
        case "--system":
            if let v = iter.next() { args.system = v }
        case "--voice":
            if let v = iter.next() { args.voice = v }
        case "--help", "-h":
            printUsage()
            exit(0)
        default:
            fputs("voicelab: unrecognised argument: \(arg)\n", stderr)
            printUsage()
            exit(2)
        }
    }
    return args
}

func printUsage() {
    fputs("""
voicelab — Grok Realtime voice loop with OS-level AEC (VoiceProcessingIO).

Usage:
  voicelab [--continuous] [--voice <name>] [--system <prompt>] [-v]

Modes:
  default       Push-to-talk. Press Enter to talk, Enter again to send.
  --continuous  Always-on server VAD.

Other:
  --voice       Grok TTS voice (default: Eve)
  --system      System prompt
  -v            Verbose protocol logging

Requires xai-api-key in the macOS keychain:
  security add-generic-password -a jevons -s xai-api-key -w <key>

""", stderr)
}

func fatal(_ msg: String) -> Never {
    fputs("voicelab: \(msg)\n", stderr)
    exit(1)
}

// MARK: - main

let args = parseArgs()

let apiKey: String
do {
    apiKey = try Keychain.lookup(service: "xai-api-key")
} catch {
    fatal("\(error.localizedDescription)")
}

let mode: VoiceLoop.Mode = args.continuous ? .continuous : .pushToTalk

let loop: VoiceLoop
do {
    loop = try VoiceLoop(config: .init(
        apiKey: apiKey,
        voice: args.voice,
        systemPrompt: args.system,
        mode: mode
    ))
} catch {
    fatal("\(error.localizedDescription)")
}

// Shared state for the CLI UX.
final class State: @unchecked Sendable {
    var responseDoneSignal: () -> Void = {}
    var sessionReadySignal: () -> Void = {}
}
let state = State()

loop.onSessionReady = {
    state.sessionReadySignal()
}
loop.onUserTranscript = { text in
    print("\n> \(text.trimmingCharacters(in: .whitespacesAndNewlines))")
}
loop.onAssistantTranscriptDelta = { delta in
    print(delta, terminator: "")
    fflush(stdout)
}
loop.onAssistantTranscriptDone = {
    print()
}
loop.onResponseDone = {
    state.responseDoneSignal()
}
loop.onError = { err in
    fputs("\nvoicelab error: \(err.localizedDescription)\n", stderr)
}

signal(SIGINT) { _ in
    fputs("\nvoicelab: shutting down\n", stderr)
    // Process.exit lets atexit + deferred fire; the engine teardown
    // happens on Process exit, which is fine for a CLI.
    exit(0)
}
signal(SIGTERM) { _ in exit(0) }

// Connect + start the engine. Tie session-ready to a semaphore so we
// don't print the PTT prompt before Grok is ready to receive audio.
let readySem = DispatchSemaphore(value: 0)
state.sessionReadySignal = { readySem.signal() }

let connectTask = Task {
    do {
        try await loop.start()
    } catch {
        fatal("start: \(error.localizedDescription)")
    }
}

readySem.wait()
_ = connectTask // keep alive; engine + grok have their own lifetimes now

switch mode {
case .continuous:
    fputs("voicelab (continuous): session ready — start talking. Ctrl-C to quit.\n", stderr)
    fputs("  AEC + NS + AGC via AVAudioEngine voice processing.\n", stderr)
    // Block until SIGINT.
    dispatchMain()

case .pushToTalk:
    fputs("voicelab (push-to-talk): session ready.\n", stderr)
    fputs("  press Enter to talk, Enter again to send. Ctrl-C to quit.\n", stderr)

    let responseSem = DispatchSemaphore(value: 0)
    state.responseDoneSignal = { responseSem.signal() }

    while true {
        fputs("\n[press Enter to talk] ", stderr)
        guard readLine(strippingNewline: true) != nil else { break }

        loop.startTalking()
        fputs("🎤 listening — press Enter to send ", stderr)
        guard readLine(strippingNewline: true) != nil else { break }

        let endTask = Task {
            do {
                try await loop.endTalking()
            } catch {
                fputs("\nvoicelab: end-talking failed: \(error.localizedDescription)\n", stderr)
            }
        }
        _ = endTask

        fputs("💭 thinking…\n", stderr)
        responseSem.wait()
    }
}
