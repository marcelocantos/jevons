// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import Pigeon
import SwiftUI

struct ContentView: View {
    @Environment(Connection.self) private var connection
    @Environment(VoiceManager.self) private var voiceManager
    @State private var showSheet = false
    @State private var showNotification = false
    @State private var pairingArtifact: PairingArtifact? = PigeonAccount.shared.load()

    var body: some View {
        Group {
            if let artifact = pairingArtifact, !artifact.isExpired() {
                // Production path: canonical React over the pigeon HTTP/WS
                // tunnel, authenticated by the persisted PairingArtifact.
                WebUIView(artifact: artifact)
                    .ignoresSafeArea()
            } else if case .disconnected = connection.state {
                // A remembered endpoint does not reconnect after an
                // explicit disconnect. Never fall through to ServerView.
                ConnectView(onPaired: { pairingArtifact = $0 })
            } else if let url = connection.httpBaseURL {
                // Direct path: load the daemon's canonical React origin.
                // ServerView and any bundled web/index.html cannot intercept.
                WebUIView(serverURL: url)
                    .ignoresSafeArea()
            } else {
                // Connecting / error before an HTTP base URL is known.
                fallbackView
            }
        }
        .onChange(of: connection.sheetView != nil) { _, hasSheet in
            showSheet = hasSheet
        }
        .sheet(isPresented: $showSheet, onDismiss: {
            // If the user dismisses via swipe, tell the server.
            if connection.sheetView != nil {
                connection.sendAction("dismiss_sheet")
            }
        }) {
            if let sheetView = connection.sheetView {
                // Sheets remain native for the residual server-driven
                // chrome that is not the cockpit path (🎯T628).
                ServerView(node: sheetView) { action, value in
                    connection.sendAction(action, value: value)
                }
            }
        }
        .onChange(of: connection.notificationTitle != nil) { _, hasNotification in
            showNotification = hasNotification
        }
        .alert(
            connection.notificationTitle ?? "",
            isPresented: $showNotification,
            actions: { Button("OK") { connection.dismissNotification() } },
            message: { Text(connection.notificationBody ?? "") }
        )
    }

    @ViewBuilder
    private var fallbackView: some View {
        switch connection.state {
        case .disconnected:
            ConnectView(onPaired: { pairingArtifact = $0 })
        case .connecting:
            ProgressView("Connecting...")
        case .connected:
            // httpBaseURL should already have won above; keep a
            // non-cockpit placeholder only if the URL is still missing.
            ProgressView("Loading cockpit...")
        case .error:
            if connection.hasConnected {
                ProgressView("Reconnecting...")
            } else {
                ConnectView(onPaired: { pairingArtifact = $0 })
            }
        }
    }
}
