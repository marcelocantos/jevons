// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import Foundation

struct HeadSection: Equatable {
    let providerID: String
    let title: String
}

/// Thin /ws/remote client: applies view frames into section list.
/// Section derivation mirrors internal/desktop.SectionsFromComposedRoot
/// (provider/{id}/{surface} children → one section per provider).
final class HeadClient {
    private let baseURL: String
    private let onSections: ([HeadSection]) -> Void
    private var task: URLSessionWebSocketTask?
    private var session: URLSession?
    private var stopped = false

    init(baseURL: String, onSections: @escaping ([HeadSection]) -> Void) {
        self.baseURL = baseURL
        self.onSections = onSections
    }

    func start() {
        stopped = false
        connect()
    }

    func stop() {
        stopped = true
        task?.cancel(with: .goingAway, reason: nil)
        task = nil
    }

    private func connect() {
        guard !stopped else { return }
        var urlString = baseURL
        if urlString.hasPrefix("http://") {
            urlString = "ws://" + urlString.dropFirst("http://".count)
        } else if urlString.hasPrefix("https://") {
            urlString = "wss://" + urlString.dropFirst("https://".count)
        }
        if urlString.hasSuffix("/") {
            urlString = String(urlString.dropLast())
        }
        urlString += "/ws/remote"
        guard let url = URL(string: urlString) else { return }

        let session = URLSession(configuration: .default)
        self.session = session
        let task = session.webSocketTask(with: url)
        self.task = task
        task.resume()
        receiveLoop(task)
    }

    private func receiveLoop(_ task: URLSessionWebSocketTask) {
        task.receive { [weak self] result in
            guard let self else { return }
            switch result {
            case .failure:
                self.scheduleReconnect()
            case .success(let message):
                if case .string(let text) = message {
                    self.handleFrame(text)
                }
                self.receiveLoop(task)
            }
        }
    }

    private func handleFrame(_ text: String) {
        guard let data = text.data(using: .utf8),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let type = obj["type"] as? String else { return }
        guard type == "view" else { return }
        if let slot = obj["slot"] as? String, !slot.isEmpty, slot != "providers" {
            return
        }
        guard let root = obj["root"] as? [String: Any] else { return }
        let children = root["children"] as? [[String: Any]] ?? []
        var byProvider: [String: (title: String, order: Int)] = [:]
        var order = 0
        for child in children {
            guard let id = child["id"] as? String,
                  id.hasPrefix("provider/") else { continue }
            let rest = String(id.dropFirst("provider/".count))
            guard let slash = rest.firstIndex(of: "/") else { continue }
            let pid = String(rest[..<slash])
            var title = pid
            if let kids = child["children"] as? [[String: Any]],
               let first = kids.first,
               let props = first["props"] as? [String: Any],
               let text = props["text"] as? String, !text.isEmpty {
                title = text
            }
            if byProvider[pid] == nil {
                byProvider[pid] = (title, order)
                order += 1
            }
        }
        let sorted = byProvider.sorted { a, b in
            if a.value.order != b.value.order { return a.value.order < b.value.order }
            return a.key < b.key
        }
        onSections(sorted.map { HeadSection(providerID: $0.key, title: $0.value.title) })
    }

    private func scheduleReconnect() {
        guard !stopped else { return }
        DispatchQueue.main.asyncAfter(deadline: .now() + 2) { [weak self] in
            self?.connect()
        }
    }
}
