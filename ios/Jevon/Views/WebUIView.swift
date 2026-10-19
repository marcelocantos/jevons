// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import Pigeon
import SwiftUI
import WebKit

/// Presents the daemon's canonical cockpit.
///
/// Direct connections load the daemon HTTP origin in WKWebView, the same
/// way desktop React does. Paired connections start a loopback reverse
/// proxy that tunnels HTTP, REST and `/ws/mux` over the pigeon primary
/// stream (🎯T628), then load that local origin.
struct WebUIView: UIViewRepresentable {
    let mode: BridgeMode

    /// Convenience for the legacy direct-connection path.
    init(serverURL: URL) {
        self.mode = .direct(serverURL)
    }

    /// Artifact-driven (production) path: tunnel through pigeon.
    init(artifact: PairingArtifact) {
        self.mode = .relayArtifact(artifact)
    }

    func makeCoordinator() -> Coordinator {
        Coordinator(mode: mode)
    }

    func makeUIView(context: Context) -> WKWebView {
        let config = WKWebViewConfiguration()

        // Allow inline media (for fallback browser-mode audio if needed).
        config.allowsInlineMediaPlayback = true
        config.mediaTypesRequiringUserActionForPlayback = []

        // NOTE: programmatic input.focus() in JS does NOT bring up the
        // iOS keyboard without a prior user gesture. The private
        // WKPreferences key `keyboardDisplayRequiresUserAction` was
        // removed in iOS 26 (NSUnknownKeyException on setValue). User
        // must tap the input to surface the keyboard.

        let webView = WKWebView(frame: .zero, configuration: config)
        webView.navigationDelegate = context.coordinator
        webView.isOpaque = false
        webView.backgroundColor = .clear
        webView.scrollView.backgroundColor = .clear
        webView.scrollView.isScrollEnabled = false

        context.coordinator.start(in: webView)

        return webView
    }

    func updateUIView(_ webView: WKWebView, context: Context) {
        context.coordinator.update(mode: mode, in: webView)
    }

    static func dismantleUIView(_ webView: WKWebView, coordinator: Coordinator) {
        coordinator.cancelRetry()
        coordinator.shutdown()
        webView.stopLoading()
        webView.navigationDelegate = nil
    }

    @MainActor
    final class Coordinator: NSObject, WKNavigationDelegate {
        private var mode: BridgeMode
        private var loadedURL: URL?
        private var retryTask: Task<Void, Never>?
        private var retryDelay: TimeInterval = 1
        private static let maximumRetryDelay: TimeInterval = 8

        private var tunnel: PigeonHTTPTunnel?
        private var startTask: Task<Void, Never>?

        init(mode: BridgeMode) {
            self.mode = mode
            super.init()
        }

        func start(in webView: WKWebView) {
            switch mode {
            case .direct(let serverURL):
                load(serverURL, in: webView)
            case .relayArtifact(let artifact):
                startTunnel(artifact: artifact, in: webView)
            }
        }

        func update(mode: BridgeMode, in webView: WKWebView) {
            switch (self.mode, mode) {
            case let (.direct(old), .direct(new)) where old == new:
                return
            case let (.relayArtifact(old), .relayArtifact(new))
                where old.record.peerInstanceID == new.record.peerInstanceID
                && old.expiresAt == new.expiresAt:
                return
            default:
                break
            }
            shutdown()
            self.mode = mode
            start(in: webView)
        }

        func load(_ url: URL, in webView: WKWebView) {
            guard loadedURL != url else { return }
            cancelRetry()
            loadedURL = url
            retryDelay = 1
            webView.load(URLRequest(url: url))
        }

        func cancelRetry() {
            retryTask?.cancel()
            retryTask = nil
        }

        func shutdown() {
            cancelRetry()
            startTask?.cancel()
            startTask = nil
            tunnel?.close()
            tunnel = nil
            loadedURL = nil
        }

        func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
            cancelRetry()
            retryDelay = 1
        }

        func webView(
            _ webView: WKWebView,
            didFailProvisionalNavigation navigation: WKNavigation!,
            withError error: Error
        ) {
            retryNavigation(webView, error: error)
        }

        func webView(
            _ webView: WKWebView,
            didFail navigation: WKNavigation!,
            withError error: Error
        ) {
            retryNavigation(webView, error: error)
        }

        private func retryNavigation(_ webView: WKWebView, error: Error) {
            guard (error as NSError).code != NSURLErrorCancelled,
                  let url = loadedURL else { return }
            cancelRetry()
            let delay = retryDelay
            retryDelay = min(retryDelay * 2, Self.maximumRetryDelay)
            retryTask = Task { [weak self, weak webView] in
                do {
                    try await Task.sleep(for: .seconds(delay))
                } catch {
                    return
                }
                guard let self, let webView, self.loadedURL == url else { return }
                webView.load(URLRequest(url: url))
            }
        }

        private func startTunnel(artifact: PairingArtifact, in webView: WKWebView) {
            startTask?.cancel()
            startTask = Task { [weak self, weak webView] in
                guard let self, let webView else { return }
                do {
                    let tunnel = try await PigeonHTTPTunnel.connect(artifact: artifact)
                    guard !Task.isCancelled else {
                        tunnel.close()
                        return
                    }
                    self.tunnel = tunnel
                    self.load(tunnel.localURL, in: webView)
                } catch {
                    // Surface a blank page with retry via the navigation
                    // failure path once a placeholder load is attempted.
                    // Keep the error visible in the system log for device labs.
                    let logger = Logger(subsystem: "com.marcelocantos.jevons", category: "WebUIView")
                    logger.error("pigeon HTTP tunnel failed: \(error.localizedDescription, privacy: .public)")
                }
            }
        }
    }
}

import os
