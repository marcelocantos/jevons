// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import Foundation
import Security

/// Cross-platform secret lookup via the Security framework. Same
/// account/service convention jevonsd uses (`-a jevons -s <service>`)
/// — on macOS the items written by the `security` CLI live in the
/// same kSecClassGenericPassword bucket this reads.
public enum Keychain {
    public enum Error: Swift.Error, LocalizedError {
        case notFound(String)
        case osStatus(OSStatus)
        case decode

        public var errorDescription: String? {
            switch self {
            case .notFound(let s): "keychain: '\(s)' not found"
            case .osStatus(let s): "keychain: OSStatus \(s)"
            case .decode: "keychain: decode failed"
            }
        }
    }

    public static func lookup(service: String, account: String = "jevons") throws -> String {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne,
        ]
        var item: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &item)
        guard status == errSecSuccess else {
            if status == errSecItemNotFound {
                throw Error.notFound(service)
            }
            throw Error.osStatus(status)
        }
        guard let data = item as? Data,
              let value = String(data: data, encoding: .utf8) else {
            throw Error.decode
        }
        return value.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// Save (or overwrite) a value under the given account/service.
    /// Used by the iOS shell after the user injects the API key via
    /// env var or pastes it in once.
    public static func save(_ value: String, service: String, account: String = "jevons") throws {
        let data = Data(value.utf8)
        let base: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]
        // Try update first; fall back to add if not present.
        let updateStatus = SecItemUpdate(base as CFDictionary, [kSecValueData as String: data] as CFDictionary)
        if updateStatus == errSecSuccess { return }
        if updateStatus != errSecItemNotFound {
            throw Error.osStatus(updateStatus)
        }
        var addQuery = base
        addQuery[kSecValueData as String] = data
        let addStatus = SecItemAdd(addQuery as CFDictionary, nil)
        guard addStatus == errSecSuccess else {
            throw Error.osStatus(addStatus)
        }
    }
}
