// SPDX-License-Identifier: MIT
import Foundation
import Network
import XCTest
@testable import WireGuardKit

private final class DNSAnswers {
    let records: UnsafeMutablePointer<addrinfo>
    private let count: Int
    private var cleanup: [() -> Void] = []

    init(_ addresses: [(String, UInt32)]) {
        count = addresses.count
        records = .allocate(capacity: max(count, 1))
        records.initialize(repeating: addrinfo(), count: max(count, 1))
        for (index, entry) in addresses.enumerated() {
            if entry.0.contains(":") {
                let address = UnsafeMutablePointer<sockaddr_in6>.allocate(capacity: 1)
                address.initialize(to: sockaddr_in6())
                address.pointee.sin6_family = sa_family_t(AF_INET6)
                address.pointee.sin6_len = UInt8(MemoryLayout<sockaddr_in6>.size)
                address.pointee.sin6_scope_id = entry.1
                XCTAssertEqual(inet_pton(AF_INET6, entry.0, &address.pointee.sin6_addr), 1)
                records[index].ai_family = AF_INET6
                records[index].ai_addrlen = socklen_t(MemoryLayout<sockaddr_in6>.size)
                records[index].ai_addr = UnsafeMutableRawPointer(address).assumingMemoryBound(to: sockaddr.self)
                cleanup.append { address.deinitialize(count: 1); address.deallocate() }
            } else {
                let address = UnsafeMutablePointer<sockaddr_in>.allocate(capacity: 1)
                address.initialize(to: sockaddr_in())
                address.pointee.sin_family = sa_family_t(AF_INET)
                address.pointee.sin_len = UInt8(MemoryLayout<sockaddr_in>.size)
                XCTAssertEqual(inet_pton(AF_INET, entry.0, &address.pointee.sin_addr), 1)
                records[index].ai_family = AF_INET
                records[index].ai_addrlen = socklen_t(MemoryLayout<sockaddr_in>.size)
                records[index].ai_addr = UnsafeMutableRawPointer(address).assumingMemoryBound(to: sockaddr.self)
                cleanup.append { address.deinitialize(count: 1); address.deallocate() }
            }
            if index + 1 < count { records[index].ai_next = records.advanced(by: index + 1) }
        }
    }
    deinit {
        cleanup.forEach { $0() }
        records.deinitialize(count: max(count, 1)); records.deallocate()
    }
}

final class DNSResolverTests: XCTestCase {
    private let endpoint = Endpoint(host: .name("example.invalid", nil), port: 51820)

    func testEmptySuccessfulLookupReturnsDNSError() {
        XCTAssertThrowsError(try DNSResolver.selectAddress(from: nil, endpoint: endpoint,
            address: "example.invalid", preferIPv4: true)) { error in
            XCTAssertEqual((error as? DNSResolutionError)?.errorCode, EAI_NONAME)
        }
    }

    func testShortAndNullSockaddrsAreRejected() {
        let answers = DNSAnswers([("192.0.2.1", 0), ("2001:db8::1", 0)])
        answers.records[0].ai_addrlen -= 1
        answers.records[1].ai_addrlen -= 1
        XCTAssertNil(IPv4Address(addrInfo: answers.records[0]))
        XCTAssertNil(IPv6Address(addrInfo: answers.records[1]))
        answers.records[0].ai_addrlen += 1
        answers.records[0].ai_addr = nil
        XCTAssertNil(IPv4Address(addrInfo: answers.records[0]))
        XCTAssertThrowsError(try DNSResolver.selectAddress(from: answers.records, endpoint: endpoint,
            address: "example.invalid", preferIPv4: false))
    }

    func testUnsupportedFirstRecordDoesNotHideUsableAnswer() throws {
        let answers = DNSAnswers([("192.0.2.1", 0), ("2001:db8::2", 0)])
        answers.records[0].ai_family = AF_UNIX
        let result = try DNSResolver.selectAddress(from: answers.records, endpoint: endpoint,
            address: "example.invalid", preferIPv4: false)
        XCTAssertEqual(result.stringRepresentation, "[2001:db8::2]:51820")
    }

    func testIPv4PreferenceAndDNS64SystemOrderAreSeparate() throws {
        let answers = DNSAnswers([("64:ff9b::c000:201", 0), ("192.0.2.1", 0)])
        let initial = try DNSResolver.selectAddress(from: answers.records, endpoint: endpoint,
            address: "example.invalid", preferIPv4: true)
        let synthesized = try DNSResolver.selectAddress(from: answers.records, endpoint: endpoint,
            address: "example.invalid", preferIPv4: false)
        XCTAssertEqual(initial.host, .ipv4(IPv4Address("192.0.2.1")!))
        XCTAssertEqual(synthesized.host, .ipv6(IPv6Address("64:ff9b::c000:201")!))
        XCTAssertEqual(initial.port, endpoint.port)
        XCTAssertEqual(synthesized.port, endpoint.port)
    }

    func testIPv6ScopeIsPreserved() throws {
        let scope = if_nametoindex("lo0")
        XCTAssertNotEqual(scope, 0)
        let answers = DNSAnswers([("fe80::1", scope)])
        let address = try XCTUnwrap(IPv6Address(addrInfo: answers.records[0]))
        XCTAssertEqual(address.interface?.index, Int(scope))
    }

    func testInvalidHostDoesNotReachCStringLookup() {
        for name in ["", "example.invalid\0suffix"] {
            let endpoint = Endpoint(host: .name(name, nil), port: 51820)
            XCTAssertThrowsError(try DNSResolver.resolveSync(endpoint: endpoint)) { error in
                XCTAssertEqual((error as? DNSResolutionError)?.errorCode, EAI_NONAME)
            }
        }
        XCTAssertNil(Endpoint(from: ":51820"))
        XCTAssertNil(Endpoint(from: "[]:51820"))
    }

    func testNumericFastPathPreservesPositions() throws {
        let ipv4 = try XCTUnwrap(Endpoint(from: "192.0.2.1:51820"))
        let ipv6 = try XCTUnwrap(Endpoint(from: "[2001:db8::1]:443"))
        let result = DNSResolver.resolveSync(endpoints: [ipv4, nil, ipv6])
        XCTAssertEqual(try result[0]?.get(), ipv4)
        XCTAssertNil(result[1])
        XCTAssertEqual(try result[2]?.get(), ipv6)
    }
}
