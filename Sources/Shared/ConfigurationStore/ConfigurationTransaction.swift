// SPDX-License-Identifier: MIT
import Foundation

/// Keeps both Keychain generations alive until NetworkExtension confirms which
/// reference is persisted. All callbacks and model changes run on MainActor.
/// Canonical aliases for the same item are not new generations: pass nil for
/// them, so neither success nor rollback deletes their shared backing item.
@MainActor
final class ConfigurationTransaction {
    private let oldReference: Data?
    private let newReference: Data?
    private let restore: () -> Void
    private let delete: (Data) -> Void
    private var finished = false

    init(oldReference: Data?, newReference: Data?, restore: @escaping () -> Void,
         delete: @escaping (Data) -> Void) {
        self.oldReference = oldReference
        self.newReference = newReference
        self.restore = restore
        self.delete = delete
    }

    /// false means a duplicate completion; callers must not complete twice.
    func finish(error: Error?) -> Bool {
        guard !finished else { return false }
        finished = true
        if error != nil { restore() }
        if oldReference != newReference, let obsolete = error == nil ? oldReference : newReference {
            delete(obsolete)
        }
        return true
    }
}
