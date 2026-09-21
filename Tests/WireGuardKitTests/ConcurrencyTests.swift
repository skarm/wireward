// SPDX-License-Identifier: MIT

import Foundation
import XCTest
@testable import WireGuardKit

final class ConcurrencyTests: XCTestCase {
    func testConcurrentMapPreservesOrderAndInvokesTransformOnce() {
        let inputs = Array(0..<10_000)
        let visits = LockedValue([Int](repeating: 0, count: inputs.count))
        let result = inputs.concurrentMap(queue: .global()) { value in
            visits.withLock { $0[value] += 1 }
            return value * value
        }
        XCTAssertEqual(result, inputs.map { $0 * $0 })
        XCTAssertEqual(visits.withLock { $0 }, Array(repeating: 1, count: inputs.count))
    }

    func testConcurrentMapRetainsNilResultsAndHandlesEmptyInput() {
        let result: [Int?] = [1, 2, 3, 4].concurrentMap(queue: .global()) { $0.isMultiple(of: 2) ? $0 : nil }
        XCTAssertEqual(result, [nil, 2, nil, 4])
        let empty: [Int] = [].concurrentMap(queue: .global()) { $0 }
        XCTAssertTrue(empty.isEmpty)
        XCTAssertEqual([1, 2, 3].concurrentMap(queue: nil) { $0 * 2 }, [2, 4, 6])
    }

    func testLockSerializesReadModifyWrite() {
        let count = LockedValue(0)
        DispatchQueue.concurrentPerform(iterations: 10_000) { _ in
            count.withLock { $0 += 1 }
        }
        XCTAssertEqual(count.withLock { $0 }, 10_000)
    }

    func testLockIsReleasedWhenBodyThrows() {
        enum Failure: Error { case expected }
        let value = LockedValue(1)
        XCTAssertThrowsError(try value.withLock { _ in throw Failure.expected })
        value.withLock { $0 += 1 }
        XCTAssertEqual(value.withLock { $0 }, 2)
    }

    func testTunnelConfigurationIsAnIndependentSnapshot() throws {
        let key = try XCTUnwrap(PrivateKey(rawValue: Data(repeating: 1, count: 32)))
        let original = TunnelConfiguration(name: "original", interface: InterfaceConfiguration(privateKey: key), peers: [])
        var copy = original
        copy.name = "edited"
        copy.interface.listenPort = 12345
        XCTAssertEqual(original.name, "original")
        XCTAssertNil(original.interface.listenPort)
        XCTAssertNotEqual(original, copy)
    }

    func testImmutableKeysKeepRFC7748DerivationAndEncodings() throws {
        let key = try XCTUnwrap(PrivateKey(hexKey: "77076d0a7318a57d3c16c17251b26645df4c2f87ebc0992ab177fba51db92c2a"))
        XCTAssertEqual(key.publicKey.hexKey, "8520f0098930a754748b7ddcb43ef75a0dbf3a0d26381af4eba4a98eaa9b4e6a")
        XCTAssertEqual(PrivateKey(base64Key: key.base64Key), key)
        XCTAssertEqual(Set([key.publicKey, key.publicKey]).count, 1)
        XCTAssertNil(PrivateKey(rawValue: Data(repeating: 0, count: 31)))
        XCTAssertNil(PublicKey(rawValue: Data(repeating: 0, count: 33)))
        XCTAssertNil(PreSharedKey(hexKey: "invalid"))
    }
}
