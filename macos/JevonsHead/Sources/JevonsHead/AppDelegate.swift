// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import AppKit

/// Owns the status item and reconnects the WS client. Menu items mirror
/// the head MenuModel sections (one per enabled provider).
final class AppDelegate: NSObject, NSApplicationDelegate {
    private var statusItem: NSStatusItem!
    private var menu: NSMenu!
    private var client: HeadClient!
    private var sections: [HeadSection] = []

    func applicationDidFinishLaunching(_ notification: Notification) {
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
        if let button = statusItem.button {
            button.image = NSImage(systemSymbolName: "hexagon", accessibilityDescription: "Jevons")
            button.image?.isTemplate = true
        }
        menu = NSMenu()
        menu.autoenablesItems = false
        statusItem.menu = menu
        rebuildMenu()

        let base = ProcessInfo.processInfo.environment["JEVONS_URL"]
            ?? "http://127.0.0.1:13705"
        client = HeadClient(baseURL: base) { [weak self] secs in
            DispatchQueue.main.async {
                self?.sections = secs
                self?.rebuildMenu()
            }
        }
        client.start()
    }

    private func rebuildMenu() {
        menu.removeAllItems()
        if sections.isEmpty {
            let item = NSMenuItem(title: "No providers", action: nil, keyEquivalent: "")
            item.isEnabled = false
            menu.addItem(item)
        } else {
            for s in sections {
                let item = NSMenuItem(title: s.title, action: nil, keyEquivalent: "")
                item.isEnabled = false
                item.toolTip = "provider \(s.providerID)"
                menu.addItem(item)
            }
        }
        menu.addItem(NSMenuItem.separator())
        let quit = NSMenuItem(title: "Quit Jevons Head", action: #selector(quit), keyEquivalent: "q")
        quit.target = self
        menu.addItem(quit)
    }

    @objc private func quit() {
        NSApp.terminate(nil)
    }
}
