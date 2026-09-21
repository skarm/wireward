// SPDX-License-Identifier: MIT
// Copyright © 2018-2023 WireGuard LLC. All Rights Reserved.

import Foundation
import Security

class Keychain {
    enum Failure: LocalizedError {
        case status(OSStatus)
        case invalidResult
        case invalidAppConfiguration

        var errorDescription: String? {
            switch self {
            case .status(let status): return "Keychain operation failed (status \(status))."
            case .invalidResult: return "Keychain returned an invalid configuration reference."
            case .invalidAppConfiguration: return "The application could not prepare Keychain access."
            }
        }
    }

    static func openReference(called ref: Data) -> String? {
        var result: CFTypeRef?
        let ret = SecItemCopyMatching([kSecValuePersistentRef: ref,
                                        kSecReturnData: true] as CFDictionary,
                                       &result)
        if ret != errSecSuccess || result == nil {
            wg_log(.error, message: "Unable to open config from keychain: \(ret)")
            return nil
        }
        guard let data = result as? Data else { return nil }
        return String(data: data, encoding: String.Encoding.utf8)
    }

    static func makeReference(containing value: String, called name: String) throws -> Data {
        var ret: OSStatus
        guard var bundleIdentifier = Bundle.main.bundleIdentifier else {
            wg_log(.error, staticMessage: "Unable to determine bundle identifier")
            throw Failure.invalidAppConfiguration
        }
        if bundleIdentifier.hasSuffix(".network-extension") {
            bundleIdentifier.removeLast(".network-extension".count)
        }
        let itemLabel = "WireGuard Tunnel: \(name)"
        var items: [CFString: Any] = [kSecClass: kSecClassGenericPassword,
                                    kSecAttrLabel: itemLabel,
                                    kSecAttrAccount: name + ": " + UUID().uuidString,
                                    kSecAttrDescription: "wg-quick(8) config",
                                    kSecAttrService: bundleIdentifier,
                                    kSecValueData: value.data(using: .utf8) as Any,
                                    kSecReturnPersistentRef: true]

        #if os(iOS)
        items[kSecAttrAccessGroup] = FileManager.appGroupId
        items[kSecAttrAccessible] = kSecAttrAccessibleAfterFirstUnlock
        #elseif os(macOS)
        items[kSecAttrSynchronizable] = false
        items[kSecAttrAccessible] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly

        guard let extensionPath = Bundle.main.builtInPlugInsURL?.appendingPathComponent("WireGuardNetworkExtension.appex", isDirectory: true).path else {
            wg_log(.error, staticMessage: "Unable to determine app extension path")
            throw Failure.invalidAppConfiguration
        }
        var extensionApp: SecTrustedApplication?
        var mainApp: SecTrustedApplication?
        ret = SecTrustedApplicationCreateFromPath(extensionPath, &extensionApp)
        if ret != kOSReturnSuccess || extensionApp == nil {
            wg_log(.error, message: "Unable to create keychain extension trusted application object: \(ret)")
            throw ret == errSecSuccess ? Failure.invalidResult : Failure.status(ret)
        }
        ret = SecTrustedApplicationCreateFromPath(nil, &mainApp)
        if ret != errSecSuccess || mainApp == nil {
            wg_log(.error, message: "Unable to create keychain local trusted application object: \(ret)")
            throw ret == errSecSuccess ? Failure.invalidResult : Failure.status(ret)
        }
        var access: SecAccess?
        ret = SecAccessCreate(itemLabel as CFString, [extensionApp!, mainApp!] as CFArray, &access)
        if ret != errSecSuccess || access == nil {
            wg_log(.error, message: "Unable to create keychain ACL object: \(ret)")
            throw ret == errSecSuccess ? Failure.invalidResult : Failure.status(ret)
        }
        items[kSecAttrAccess] = access!
        #else
        #error("Unimplemented")
        #endif

        var ref: CFTypeRef?
        ret = SecItemAdd(items as CFDictionary, &ref)
        if ret != errSecSuccess || ref == nil {
            wg_log(.error, message: "Unable to add config to keychain: \(ret)")
            throw ret == errSecSuccess ? Failure.invalidResult : Failure.status(ret)
        }
        guard let reference = ref as? Data else { throw Failure.invalidResult }
        return reference
    }

    static func deleteReference(called ref: Data) {
        let ret = SecItemDelete([kSecValuePersistentRef: ref] as CFDictionary)
        if ret != errSecSuccess {
            wg_log(.error, message: "Unable to delete config from keychain: \(ret)")
        }
    }

    static func verifyReference(called ref: Data) -> Bool {
        return SecItemCopyMatching([kSecValuePersistentRef: ref] as CFDictionary,
                                   nil) == errSecSuccess
    }
}
