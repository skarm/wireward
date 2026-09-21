// SPDX-License-Identifier: MIT
// Copyright © 2018-2023 WireGuard LLC. All Rights Reserved.

import Foundation

#if SWIFT_PACKAGE
import WireGuardKitC
#endif

/// The class describing a private key used by WireGuard.
public final class PrivateKey: BaseKey {
    public let rawValue: Data

    public init?(rawValue: Data) {
        guard rawValue.count == WG_KEY_LEN else { return nil }
        self.rawValue = rawValue
    }

    /// Derived public key
    public var publicKey: PublicKey {
        return rawValue.withUnsafeBytes { (privateKeyBufferPointer: UnsafeRawBufferPointer) -> PublicKey in
            var publicKeyData = Data(repeating: 0, count: Int(WG_KEY_LEN))
            let privateKeyBytes = privateKeyBufferPointer.baseAddress!.assumingMemoryBound(to: UInt8.self)

            publicKeyData.withUnsafeMutableBytes { (publicKeyBufferPointer: UnsafeMutableRawBufferPointer) in
                let publicKeyBytes = publicKeyBufferPointer.baseAddress!.assumingMemoryBound(to: UInt8.self)
                curve25519_derive_public_key(publicKeyBytes, privateKeyBytes)
            }

            return PublicKey(rawValue: publicKeyData)!
        }
    }

    /// Generate a new private key, propagating failure from the system RNG.
    convenience public init() throws {
        try self.init(generate: { curve25519_generate_private_key($0) })
    }

    convenience init(generate: (UnsafeMutablePointer<UInt8>) -> Int32) throws {
        var privateKeyData = Data(repeating: 0, count: Int(WG_KEY_LEN))
        let status = privateKeyData.withUnsafeMutableBytes { (buffer: UnsafeMutableRawBufferPointer) in
            generate(buffer.baseAddress!.assumingMemoryBound(to: UInt8.self))
        }
        guard status == 0 else {
            privateKeyData.resetBytes(in: 0..<privateKeyData.count)
            throw PrivateKeyGenerationError.randomBytes(status)
        }
        self.init(rawValue: privateKeyData)!
    }
}

/// The class describing a public key used by WireGuard.
public final class PublicKey: BaseKey {
    public let rawValue: Data

    public init?(rawValue: Data) {
        guard rawValue.count == WG_KEY_LEN else { return nil }
        self.rawValue = rawValue
    }
}

/// The class describing a pre-shared key used by WireGuard.
public final class PreSharedKey: BaseKey {
    public let rawValue: Data

    public init?(rawValue: Data) {
        guard rawValue.count == WG_KEY_LEN else { return nil }
        self.rawValue = rawValue
    }
}

/// Shared operations for immutable, Sendable WireGuard key values.
public protocol BaseKey: RawRepresentable, Equatable, Hashable, Sendable where RawValue == Data {}

extension BaseKey {
    /// Hex encoded representation
    public var hexKey: String {
        return rawValue.withUnsafeBytes { (rawBufferPointer: UnsafeRawBufferPointer) -> String in
            let inBytes = rawBufferPointer.baseAddress!.assumingMemoryBound(to: UInt8.self)
            var outBytes = [CChar](repeating: 0, count: Int(WG_KEY_LEN_HEX))
            key_to_hex(&outBytes, inBytes)
            return String(cString: outBytes, encoding: .ascii)!
        }
    }

    /// Base64 encoded representation
    public var base64Key: String {
        return rawValue.withUnsafeBytes { (rawBufferPointer: UnsafeRawBufferPointer) -> String in
            let inBytes = rawBufferPointer.baseAddress!.assumingMemoryBound(to: UInt8.self)
            var outBytes = [CChar](repeating: 0, count: Int(WG_KEY_LEN_BASE64))
            key_to_base64(&outBytes, inBytes)
            return String(cString: outBytes, encoding: .ascii)!
        }
    }

    /// Initialize the key with hex representation
    public init?(hexKey: String) {
        guard hexKey.utf8.count == Int(WG_KEY_LEN_HEX) - 1 else { return nil }
        var bytes = Data(repeating: 0, count: Int(WG_KEY_LEN))
        let success = bytes.withUnsafeMutableBytes { (bufferPointer: UnsafeMutableRawBufferPointer) -> Bool in
            return key_from_hex(bufferPointer.baseAddress!.assumingMemoryBound(to: UInt8.self), hexKey)
        }
        if success {
            self.init(rawValue: bytes)
        } else {
            return nil
        }
    }

    /// Initialize the key with base64 representation
    public init?(base64Key: String) {
        guard base64Key.utf8.count == Int(WG_KEY_LEN_BASE64) - 1 else { return nil }
        var bytes = Data(repeating: 0, count: Int(WG_KEY_LEN))
        let success = bytes.withUnsafeMutableBytes { (bufferPointer: UnsafeMutableRawBufferPointer) -> Bool in
            return key_from_base64(bufferPointer.baseAddress!.assumingMemoryBound(to: UInt8.self), base64Key)
        }
        if success {
            self.init(rawValue: bytes)
        } else {
            return nil
        }
    }

    public func hash(into hasher: inout Hasher) {
        hasher.combine(rawValue)
    }

    public static func == (lhs: Self, rhs: Self) -> Bool {
        return lhs.rawValue.withUnsafeBytes { (lhsBytes: UnsafeRawBufferPointer) -> Bool in
            return rhs.rawValue.withUnsafeBytes { (rhsBytes: UnsafeRawBufferPointer) -> Bool in
                return key_eq(
                    lhsBytes.baseAddress!.assumingMemoryBound(to: UInt8.self),
                    rhsBytes.baseAddress!.assumingMemoryBound(to: UInt8.self)
                )
            }
        }
    }
}

public enum PrivateKeyGenerationError: LocalizedError {
    case randomBytes(Int32)

    public var errorDescription: String? {
        switch self {
        case .randomBytes(let code):
            return "The system random number generator could not create a private key (error \(code))."
        }
    }
}
