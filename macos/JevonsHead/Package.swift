// swift-tools-version:5.9
// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import PackageDescription

// JevonsHead is the macOS menu-bar chrome for 🎯T27.7 — a thin AppKit
// status item over jevonsd's /ws/remote composed provider view. Section
// membership is driven by the server model (N providers → N menu items);
// UX feel is the class-3 residual. Built as an SPM executable for dev.
let package = Package(
    name: "JevonsHead",
    platforms: [.macOS(.v13)],
    targets: [
        .executableTarget(
            name: "JevonsHead",
            path: "Sources/JevonsHead"
        )
    ]
)
