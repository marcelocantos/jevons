// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import Darwin
import Foundation
import VoicelabKit

struct CLIArgs {
    var system: String = "You are jevons, a voice-first assistant. Keep replies brief and conversational."
    var voice: String = "Eve"
    var triggerDB: Double = -38
    var silenceSec: Int = 10
}

func parseArgs() -> CLIArgs {
    var args = CLIArgs()
    var iter = CommandLine.arguments.dropFirst().makeIterator()
    while let arg = iter.next() {
        switch arg {
        case "--system":
            if let v = iter.next() { args.system = v }
        case "--voice":
            if let v = iter.next() { args.voice = v }
        case "--trigger-db":
            if let v = iter.next(), let d = Double(v) { args.triggerDB = d }
        case "--silence-sec":
            if let v = iter.next(), let n = Int(v) { args.silenceSec = n }
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
voicelab — episodic Grok Realtime voice loop with OS-level AEC.

Usage:
  voicelab [--voice <name>] [--system <prompt>]
           [--trigger-db <dBFS>] [--silence-sec <N>]

Idle until local VAD detects you speaking; opens a Grok session,
holds it open while turns continue, closes it after a silent
gap. Ctrl-C to quit.

  --trigger-db   activation threshold in dBFS (default -38)
  --silence-sec  silence after response.done before closing
                 the Grok session (default 10s)

Requires xai-api-key in the macOS keychain:
  security add-generic-password -a jevons -s xai-api-key -w <key>

""", stderr)
}

func fatal(_ msg: String) -> Never {
    fputs("voicelab: \(msg)\n", stderr)
    exit(1)
}

let args = parseArgs()

let apiKey: String
do {
    apiKey = try Keychain.lookup(service: "xai-api-key")
} catch {
    fatal("\(error.localizedDescription)")
}

let loop: VoiceLoop
do {
    loop = try VoiceLoop(config: .init(
        apiKey: apiKey,
        voice: args.voice,
        systemPrompt: args.system,
        vad: LocalVAD(triggerDB: args.triggerDB),
        postResponseSilenceMs: args.silenceSec * 1000
    ))
} catch {
    fatal("\(error.localizedDescription)")
}

loop.onStateChange = { state in
    fputs("voicelab: state → \(state.rawValue)\n", stderr)
}
loop.onSessionReady = {
    fputs("voicelab: session ready\n", stderr)
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
loop.onError = { err in
    fputs("\nvoicelab error: \(err.localizedDescription)\n", stderr)
}

signal(SIGINT) { _ in
    fputs("\nvoicelab: shutting down\n", stderr)
    exit(0)
}
signal(SIGTERM) { _ in exit(0) }

do {
    try loop.start()
} catch {
    fatal("start: \(error.localizedDescription)")
}

fputs("voicelab: listening. Talk to wake the session.\n", stderr)

dispatchMain()
