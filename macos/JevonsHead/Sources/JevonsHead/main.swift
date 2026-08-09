// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import AppKit

// Entry: accessory menu-bar app (no Dock icon). Connects to jevonsd and
// renders one menu section per enabled provider (🎯T27.7).
let app = NSApplication.shared
app.setActivationPolicy(.accessory)
let delegate = AppDelegate()
app.delegate = delegate
app.run()
