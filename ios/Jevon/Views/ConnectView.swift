// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import Pigeon
import SwiftUI

struct ConnectView: View {
    @Environment(Connection.self) private var connection

    /// Called after a PairingArtifact is decoded and persisted so the
    /// parent can switch to the relay WebUI path without a relaunch
    /// (🎯T7 user-flow residual).
    var onPaired: ((PairingArtifact) -> Void)?

    var body: some View {
        ZStack {
            // Full-screen QR scanner.
            #if !targetEnvironment(simulator)
            QRScannerView(
                onScanArtifact: { artifact in
                    // Persist for relaunches, then hand off immediately
                    // so ContentView can present WebUIView(artifact:).
                    do {
                        try PigeonAccount.shared.save(artifact)
                        onPaired?(artifact)
                    } catch {
                        // Leave scanner up; connection.state surfaces
                        // subsequent transport errors if any.
                    }
                }
            )
            .ignoresSafeArea()
            #endif

            // Overlay with status.
            VStack {
                Spacer()

                if case .error(let msg) = connection.state {
                    Text(msg)
                        .foregroundStyle(.white)
                        .font(.callout)
                        .padding()
                        .background(.red.opacity(0.8), in: RoundedRectangle(cornerRadius: 12))
                        .padding(.bottom, 8)
                }

                Text("Scan the QR code from jevonsd")
                    .multilineTextAlignment(.center)
                    .font(.callout)
                    .foregroundStyle(.white)
                    .padding()
                    .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 12))
                    .padding(.bottom, 40)
            }
        }
    }
}
