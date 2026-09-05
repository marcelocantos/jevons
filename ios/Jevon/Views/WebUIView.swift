// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import Pigeon
import SwiftUI
import WebKit

/// Presents the daemon's canonical cockpit. Direct connections use the
/// browser's HTTP and WebSocket transport, just like desktop React.
struct WebUIView: UIViewRepresentable {
    let mode: BridgeMode

    /// Convenience for the legacy direct-connection path.
    init(serverURL: URL) {
        self.mode = .direct(serverURL)
    }

    /// Artifact-driven (production) path: bridge connects via
    /// PigeonConn.connect(artifact:).
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

        switch mode {
        case .direct(let serverURL):
            context.coordinator.load(serverURL, in: webView)
        case .relayArtifact:
            // The paired transport replacement is tracked by T628. Keep
            // this branch explicit until it can serve canonical HTTP/WS.
            context.coordinator.bridge.attach(to: webView)
        }

        return webView
    }

    func updateUIView(_ webView: WKWebView, context: Context) {
        if case .direct(let serverURL) = mode {
            context.coordinator.load(serverURL, in: webView)
        }
    }

    static func dismantleUIView(_ webView: WKWebView, coordinator: Coordinator) {
        coordinator.cancelRetry()
        webView.stopLoading()
        webView.navigationDelegate = nil
    }

    @MainActor
    class Coordinator: NSObject, WKNavigationDelegate {
        let bridge: JevonsBridge
        private var loadedURL: URL?
        private var retryTask: Task<Void, Never>?
        private var retryDelay: TimeInterval = 1
        private static let maximumRetryDelay: TimeInterval = 8

        init(mode: BridgeMode) {
            bridge = JevonsBridge(mode: mode)
            super.init()
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

        func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
            cancelRetry()
            retryDelay = 1
        }

        func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
            retryNavigation(webView, error: error)
        }

        func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) {
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
    }
}
