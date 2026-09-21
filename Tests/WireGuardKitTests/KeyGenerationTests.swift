// SPDX-License-Identifier: MIT
import Foundation
import XCTest
@testable import WireGuardKit

final class KeyGenerationTests: XCTestCase {
    func testKeyGenerationFailureIsReported() {
        XCTAssertThrowsError(try PrivateKey(generate: { buffer in
            buffer.initialize(repeating: 0xaa, count: 32)
            return -4300
        })) { error in
            guard case PrivateKeyGenerationError.randomBytes(-4300) = error else { return XCTFail("Lost RNG error: \(error)") }
        }
    }

    func testGeneratedKeysAreClampedAndDistinct() throws {
        let first = try PrivateKey(), second = try PrivateKey()
        XCTAssertNotEqual(first, second)
        XCTAssertEqual(first.rawValue[0] & 7, 0)
        XCTAssertEqual(first.rawValue[31] & 0xc0, 0x40)
    }

    func testKeyDecodersRejectEmbeddedNulSuffix() throws {
        let key = try PrivateKey()
        XCTAssertNil(PrivateKey(hexKey: key.hexKey + "\0ignored"))
        XCTAssertNil(PrivateKey(base64Key: key.base64Key + "\0ignored"))
        XCTAssertEqual(PrivateKey(hexKey: key.hexKey), key)
        XCTAssertEqual(PrivateKey(base64Key: key.base64Key), key)
    }
}
