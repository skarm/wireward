// SPDX-License-Identifier: MIT
// Copyright © 2018-2023 WireGuard LLC. All Rights Reserved.

import Foundation

extension Array where Element: Sendable {
    /// Maps concurrently while preserving input order. A nil queue runs serially.
    func concurrentMap<U: Sendable>(queue: DispatchQueue?, _ transform: @Sendable (Element) -> U) -> [U] {
        guard let queue = queue else { return map(transform) }
        let result = LockedValue([U?](repeating: nil, count: count))
        queue.sync {
            DispatchQueue.concurrentPerform(iterations: count) { index in
                let value = transform(self[index])
                result.withLock { $0[index] = value }
            }
        }
        return result.withLock { $0.map { $0! } }
    }
}
