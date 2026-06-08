// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import Darwin
import Foundation
import VoicelabKit

struct CLIArgs {
    var system: String = "You are jevons, a voice-first assistant. Keep replies brief and conversational."
    var voice: String = "Eve"
    var verbose: Bool = false
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
        case "-v", "--verbose":
            args.verbose = true
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
voicelab — full-duplex Grok Realtime voice loop with OS-level AEC.

Usage:
  voicelab [--voice <name>] [--system <prompt>]

Talk freely; server VAD detects when you've stopped. Ctrl-C to quit.

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
        verbose: args.verbose
    ))
} catch {
    fatal("\(error.localizedDescription)")
}

loop.onSessionReady = {
    fputs("voicelab: session ready — start talking. Ctrl-C to quit.\n", stderr)
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

Task {
    do {
        try await loop.start()
    } catch {
        fatal("start: \(error.localizedDescription)")
    }
}

dispatchMain()
