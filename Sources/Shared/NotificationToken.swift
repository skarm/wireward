// SPDX-License-Identifier: MIT
// Copyright © 2018-2023 WireGuard LLC. All Rights Reserved.

import Foundation

/// This source file contains bits of code from:
/// https://oleb.net/blog/2018/01/notificationcenter-removeobserver/

/// Wraps the observer token received from
/// `NotificationCenter.addObserver(forName:object:queue:using:)`
/// and unregisters it in deinit.
final class NotificationToken {
    let notificationCenter: NotificationCenter
    let token: Any

    init(notificationCenter: NotificationCenter = .default, token: Any) {
        self.notificationCenter = notificationCenter
        self.token = token
    }

    deinit {
        notificationCenter.removeObserver(token)
    }
}

extension NotificationCenter {
    /// Convenience wrapper for addObserver(forName:object:queue:using:)
    /// that returns our custom `NotificationToken`.
    @MainActor
    func observeOnMain(name: NSNotification.Name?, object obj: Any?, using block: @escaping @MainActor (Notification) -> Void) -> NotificationToken {
        let token = addObserver(forName: name, object: obj, queue: .main) { notification in
            // Notification may contain Objective-C objects. Delivery is explicitly on
            // OperationQueue.main and this synchronous callback never transfers them.
            nonisolated(unsafe) let mainThreadNotification = notification
            MainActor.assumeIsolated { block(mainThreadNotification) }
        }
        return NotificationToken(notificationCenter: self, token: token)
    }
}

extension Timer {
    /// The caller must add this timer to RunLoop.main.
    @MainActor
    static func forMainRunLoop(timeInterval: TimeInterval, repeats: Bool,
                              block: @escaping @MainActor (()) -> Void) -> Timer {
        Timer(timeInterval: timeInterval, repeats: repeats) { _ in
            MainActor.assumeIsolated { block(()) }
        }
    }
}
