// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

#if os(macOS)
import Foundation

/// Reads a secret from the macOS Keychain using the same
/// account/service convention jevonsd uses (`-a jevons -s <service>`).
/// Shells out to /usr/bin/security to stay consistent with the Go
/// side; the Security framework would mean entitlements + sandbox
/// gymnastics that aren't worth it for a CLI throwaway.
public enum Keychain {
    public enum Error: Swift.Error, LocalizedError {
        case notFound(String)
        case launchFailed(String)

        public var errorDescription: String? {
            switch self {
            case .notFound(let s): "keychain: '\(s)' not found"
            case .launchFailed(let s): "keychain: \(s)"
            }
        }
    }

    public static func lookup(service: String, account: String = "jevons") throws -> String {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/security")
        process.arguments = ["find-generic-password", "-a", account, "-s", service, "-w"]
        let pipe = Pipe()
        process.standardOutput = pipe
        process.standardError = Pipe()
        do {
            try process.run()
        } catch {
            throw Error.launchFailed(error.localizedDescription)
        }
        process.waitUntilExit()
        guard process.terminationStatus == 0 else {
            throw Error.notFound(service)
        }
        let data = pipe.fileHandleForReading.readDataToEndOfFile()
        guard let value = String(data: data, encoding: .utf8) else {
            throw Error.notFound(service)
        }
        return value.trimmingCharacters(in: .whitespacesAndNewlines)
    }
}
#endif
