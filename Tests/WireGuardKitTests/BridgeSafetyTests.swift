// SPDX-License-Identifier: MIT
import Foundation
import XCTest
import WireGuardKitGo
@testable import WireGuardKit

final class BridgeSafetyTests: XCTestCase {
    func testEmptySnapshotReplacesAllPeers() throws {
        let configuration = TunnelConfiguration(name: nil, interface: InterfaceConfiguration(privateKey: try PrivateKey()), peers: [])
        let settings = PacketTunnelSettingsGenerator(tunnelConfiguration: configuration, resolvedEndpoints: []).uapiConfiguration().0
        XCTAssertTrue(settings.contains("\nreplace_peers=true\n"))
        XCTAssertFalse(settings.contains("public_key="))
    }

    func testCABIRejectsInvalidHandlesAndNullArguments() {
        XCTAssertEqual(wgTurnOn(nil, -1), -Int32(EINVAL))
        XCTAssertEqual(wgTurnOn("", -1), -Int32(EBADF))
        XCTAssertEqual(wgSetConfig(-1, nil), -Int64(EINVAL))
        XCTAssertEqual(wgSetConfig(-1, ""), -Int64(EBADF))
        XCTAssertEqual(wgTurnOff(-1), -Int64(EBADF))
        XCTAssertEqual(wgBumpSockets(-1), -Int64(EBADF))
        XCTAssertEqual(wgDisableSomeRoamingForBrokenMobileSemantics(-1), -Int64(EBADF))
        XCTAssertEqual(wgGetConfig(-1, nil), -Int64(EINVAL))
        var configuration: UnsafeMutablePointer<CChar>?
        XCTAssertEqual(wgGetConfig(-1, &configuration), -Int64(EBADF))
        XCTAssertNil(configuration)
    }

    func testCABIRejectsNonTunFDWithoutClosingBorrowedFD() {
        let fd = open("/dev/null", O_RDWR)
        XCTAssertGreaterThanOrEqual(fd, 0)
        guard fd >= 0 else { return }
        defer { close(fd) }
        for _ in 0..<20 {
            XCTAssertLessThan(wgTurnOn("", fd), 0)
            XCTAssertNotEqual(fcntl(fd, F_GETFD), -1)
        }
    }

    func testVersionStringOwnership() {
        for _ in 0..<50 {
            let version = wgVersion()
            XCTAssertFalse(String(cString: version).isEmpty)
            wgFreeString(version)
        }
        wgFreeString(nil)
    }
}
