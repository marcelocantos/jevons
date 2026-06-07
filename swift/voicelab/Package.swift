// swift-tools-version:6.0
// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import PackageDescription

// The Swift voicelab. A throwaway desktop CLI for nailing the voice
// interaction loop in the same APIs that will power the iPad client —
// AVAudioEngine with voice processing, URLSession WebSocket to Grok
// Realtime, AVAudioPlayerNode for output. The CLI is just a shell
// around VoicelabKit; the iOS port wraps the same kit in SwiftUI.
let package = Package(
    name: "voicelab",
    platforms: [
        .macOS(.v14),
        .iOS(.v17),
    ],
    products: [
        .library(name: "VoicelabKit", targets: ["VoicelabKit"]),
        .executable(name: "voicelab", targets: ["voicelab"]),
    ],
    targets: [
        .target(
            name: "VoicelabKit",
            path: "Sources/VoicelabKit",
            swiftSettings: [
                // Swift 5 mode keeps strict-concurrency checking dialled
                // back. AVAudioEngine and URLSessionWebSocketTask are not
                // Sendable in 6.x; the throwaway-iteration cost of marking
                // and bridging every callback isn't worth paying yet.
                .swiftLanguageMode(.v5),
            ]
        ),
        .executableTarget(
            name: "voicelab",
            dependencies: ["VoicelabKit"],
            path: "Sources/voicelab",
            swiftSettings: [
                .swiftLanguageMode(.v5),
            ]
        ),
    ]
)
