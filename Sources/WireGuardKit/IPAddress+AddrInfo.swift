// SPDX-License-Identifier: MIT
// Copyright © 2018-2023 WireGuard LLC. All Rights Reserved.

import Foundation
import Network

extension IPv4Address {
    init?(addrInfo: addrinfo) {
        guard addrInfo.ai_family == AF_INET,
              addrInfo.ai_addrlen >= MemoryLayout<sockaddr_in>.size,
              let pointer = addrInfo.ai_addr else { return nil }
        var address = UnsafeRawPointer(pointer).loadUnaligned(as: sockaddr_in.self)
        guard address.sin_family == AF_INET else { return nil }
        self.init(Data(bytes: &address.sin_addr, count: MemoryLayout<in_addr>.size))
    }
}

extension IPv6Address {
    init?(addrInfo: addrinfo) {
        guard addrInfo.ai_family == AF_INET6,
              addrInfo.ai_addrlen >= MemoryLayout<sockaddr_in6>.size,
              let pointer = addrInfo.ai_addr else { return nil }
        var address = UnsafeRawPointer(pointer).loadUnaligned(as: sockaddr_in6.self)
        guard address.sin6_family == AF_INET6 else { return nil }
        if address.sin6_scope_id == 0 {
            self.init(Data(bytes: &address.sin6_addr, count: MemoryLayout<in6_addr>.size))
        } else {
            var text = [CChar](repeating: 0, count: Int(INET6_ADDRSTRLEN))
            guard inet_ntop(AF_INET6, &address.sin6_addr, &text, socklen_t(text.count)) != nil else { return nil }
            // A link-local address without its interface scope routes incorrectly.
            self.init(String(decoding: text.prefix { $0 != 0 }.map { UInt8(bitPattern: $0) }, as: UTF8.self) + "%\(address.sin6_scope_id)")
        }
    }
}
