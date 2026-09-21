// SPDX-License-Identifier: MIT
// Copyright © 2018-2023 WireGuard LLC. All Rights Reserved.

import Network
import Foundation

enum DNSResolver {
    private static let resolverQueue = DispatchQueue(label: "DNSResolverQueue", qos: .default, attributes: .concurrent)

    /// getaddrinfo is blocking; keep it off Swift's cooperative executor.
    static func run<Value: Sendable>(_ operation: @escaping @Sendable () -> Value) async -> Value {
        await withCheckedContinuation { continuation in
            resolverQueue.async { continuation.resume(returning: operation()) }
        }
    }

    static func resolveSync(endpoints: [Endpoint?]) -> [Result<Endpoint, DNSResolutionError>?] {
        if endpoints.allSatisfy({ $0?.hasHostAsIPAddress() ?? true }) {
            return endpoints.map { $0.map { .success($0) } }
        }
        return endpoints.concurrentMap(queue: resolverQueue) { endpoint in
            endpoint.map { endpoint in
                do throws(DNSResolutionError) { return .success(try resolveSync(endpoint: endpoint)) }
                catch { return .failure(error) }
            }
        }
    }

    static func resolveSync(endpoint: Endpoint) throws(DNSResolutionError) -> Endpoint {
        guard case .name(let name, _) = endpoint.host else { return endpoint }
        // Keep the existing IPv4 preference; AI_ALL also requests original A
        // records on DNS64 networks. Reachability policy is a separate concern.
        return try lookup(name: name, endpoint: endpoint, flags: AI_ALL, preferIPv4: true)
    }

    static func lookup(name: String, endpoint: Endpoint, flags: Int32, preferIPv4: Bool) throws(DNSResolutionError) -> Endpoint {
        guard !name.isEmpty, !name.utf8.contains(0) else {
            throw DNSResolutionError(errorCode: EAI_NONAME, address: name)
        }
        var hints = addrinfo()
        hints.ai_flags = flags
        hints.ai_family = AF_UNSPEC
        hints.ai_socktype = SOCK_DGRAM
        hints.ai_protocol = IPPROTO_UDP
        var result: UnsafeMutablePointer<addrinfo>?
        defer { if let result { freeaddrinfo(result) } }
        let code = getaddrinfo(name, "\(endpoint.port)", &hints, &result)
        guard code == 0 else { throw DNSResolutionError(errorCode: code, address: name) }
        return try selectAddress(from: result, endpoint: endpoint, address: name, preferIPv4: preferIPv4)
    }

    /// Ignore unsupported/short records and return an error for an empty usable
    /// result. A successful getaddrinfo status does not justify force-unwrapping.
    static func selectAddress(from head: UnsafeMutablePointer<addrinfo>?, endpoint: Endpoint,
                              address: String, preferIPv4: Bool) throws(DNSResolutionError) -> Endpoint {
        var next = head
        var firstIPv6: Endpoint?
        while let current = next {
            let info = current.pointee
            next = info.ai_next
            if let ipv4 = IPv4Address(addrInfo: info) {
                return Endpoint(host: .ipv4(ipv4), port: endpoint.port)
            }
            if let ipv6 = IPv6Address(addrInfo: info) {
                let resolved = Endpoint(host: .ipv6(ipv6), port: endpoint.port)
                if !preferIPv4 { return resolved }
                if firstIPv6 == nil { firstIPv6 = resolved }
            }
        }
        if let firstIPv6 { return firstIPv6 }
        throw DNSResolutionError(errorCode: EAI_NONAME, address: address)
    }
}

extension Endpoint {
    func withReresolvedIP() throws(DNSResolutionError) -> Endpoint {
        #if os(iOS)
        let name: String
        switch host {
        case .name(let hostname, _): name = hostname
        case .ipv4(let address): name = "\(address)"
        case .ipv6(let address): name = "\(address)"
        @unknown default: throw DNSResolutionError(errorCode: EAI_FAMILY, address: "unsupported endpoint")
        }
        // Preserve DNS64 synthesis and the system's order of usable addresses.
        return try DNSResolver.lookup(name: name, endpoint: self, flags: 0, preferIPv4: false)
        #else
        return self
        #endif
    }
}

public struct DNSResolutionError: LocalizedError {
    public let errorCode: Int32
    public let address: String

    init(errorCode: Int32, address: String) {
        self.errorCode = errorCode
        self.address = address
    }

    public var errorDescription: String? {
        guard let message = gai_strerror(errorCode) else { return "DNS resolution failed (\(errorCode))." }
        return String(cString: message)
    }
}
