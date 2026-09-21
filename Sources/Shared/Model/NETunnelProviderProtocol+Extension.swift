// SPDX-License-Identifier: MIT
// Copyright © 2018-2023 WireGuard LLC. All Rights Reserved.

import NetworkExtension

enum PacketTunnelProviderError: String, Error {
    case savedProtocolConfigurationIsInvalid
    case dnsResolutionFailure
    case couldNotStartBackend
    case couldNotDetermineFileDescriptor
    case couldNotSetNetworkSettings
}

extension NETunnelProviderProtocol {
    convenience init(tunnelConfiguration: TunnelConfiguration) throws {
        self.init()

        guard let name = tunnelConfiguration.name else { throw Keychain.Failure.invalidAppConfiguration }
        guard let appId = Bundle.main.bundleIdentifier else { throw Keychain.Failure.invalidAppConfiguration }
        providerBundleIdentifier = "\(appId).network-extension"
        passwordReference = try Keychain.makeReference(containing: tunnelConfiguration.asWgQuickConfig(), called: name)
        #if os(macOS)
        providerConfiguration = ["UID": getuid()]
        #endif

        let endpoints = tunnelConfiguration.peers.compactMap { $0.endpoint }
        if endpoints.count == 1 {
            serverAddress = endpoints[0].stringRepresentation
        } else if endpoints.isEmpty {
            serverAddress = "Unspecified"
        } else {
            serverAddress = "Multiple endpoints"
        }
    }

    func asTunnelConfiguration(called name: String? = nil) -> TunnelConfiguration? {
        if let passwordReference = passwordReference,
            let config = Keychain.openReference(called: passwordReference) {
            return try? TunnelConfiguration(fromWgQuickConfig: config, called: name)
        }
        if let oldConfig = providerConfiguration?["WgQuickConfig"] as? String {
            return try? TunnelConfiguration(fromWgQuickConfig: oldConfig, called: name)
        }
        return nil
    }

    func verifyConfigurationReference() -> Bool {
        guard let ref = passwordReference else { return false }
        return Keychain.verifyReference(called: ref)
    }

    @discardableResult
    func migrateConfigurationIfNeeded(called name: String) -> Bool {
        /* This is how we did things before we switched to putting items
         * in the keychain. But it's still useful to keep the migration
         * around so that .mobileconfig files are easier.
         */
        if let oldConfig = providerConfiguration?["WgQuickConfig"] as? String {
            // Do not erase the only copy when the Keychain is temporarily unavailable.
            if let reference = passwordReference {
                guard let stored = Keychain.openReference(called: reference),
                      (try? TunnelConfiguration(fromWgQuickConfig: stored, called: name)) != nil else { return false }
            } else {
                guard let reference = try? Keychain.makeReference(containing: oldConfig, called: name) else { return false }
                passwordReference = reference
            }
            #if os(macOS)
            providerConfiguration = ["UID": getuid()]
            #elseif os(iOS)
            providerConfiguration = nil
            #else
            #error("Unimplemented")
            #endif
            wg_log(.info, message: "Migrating tunnel configuration '\(name)'")
            return true
        }
        #if os(macOS)
        if passwordReference != nil && providerConfiguration?["UID"] == nil && verifyConfigurationReference() {
            providerConfiguration = ["UID": getuid()]
            return true
        }
        #elseif os(iOS)
        /* Update the stored reference from the old iOS 14 one to the canonical iOS 15 one.
         * The iOS 14 ones are 96 bits, while the iOS 15 ones are 160 bits. We do this so
         * that we can have fast set exclusion in deleteReferences safely. */
        if passwordReference != nil && passwordReference!.count == 12 {
            var result: CFTypeRef?
            let ret = SecItemCopyMatching([kSecValuePersistentRef: passwordReference!,
                                           kSecReturnPersistentRef: true] as CFDictionary,
                                           &result)
            if ret != errSecSuccess || result == nil {
                return false
            }
            guard let newReference = result as? Data else { return false }
            if !newReference.elementsEqual(passwordReference!) {
                wg_log(.info, message: "Migrating iOS 14-style keychain reference to iOS 15-style keychain reference for '\(name)'")
                passwordReference = newReference
                return true
            }
        }
        #endif
        return false
    }
}
