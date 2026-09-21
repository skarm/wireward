// SPDX-License-Identifier: MIT
import Foundation
import XCTest
@testable import ConfigurationStore

@MainActor
final class ConfigurationTransactionTests: XCTestCase {
    private let old = Data([1]), new = Data([2])
    private let failure = NSError(domain: "test", code: 1)

    func testFailedReplacementRestoresModelBeforeDeletingNewSecret() async {
        var stored = Set([old, new])
        var displayedName = "new name"
        var enabled = true
        let transaction = ConfigurationTransaction(oldReference: old, newReference: new,
            restore: { displayedName = "old name"; enabled = false },
            delete: { reference in
                XCTAssertEqual(displayedName, "old name")
                XCTAssertFalse(enabled)
                XCTAssertTrue(stored.contains(self.old))
                stored.remove(reference)
            })
        XCTAssertEqual(stored, Set([old, new]), "Neither generation can be deleted while save is pending")
        XCTAssertTrue(transaction.finish(error: failure))
        XCTAssertEqual(stored, Set([old]))
        XCTAssertFalse(transaction.finish(error: nil))
        XCTAssertEqual(stored, Set([old]))
    }

    func testSuccessfulReplacementDeletesOnlyOldSecretAfterCommit() async {
        var stored = Set([old, new])
        var persisted = old
        let transaction = ConfigurationTransaction(oldReference: old, newReference: new,
            restore: { XCTFail("Successful save rolled back") },
            delete: { reference in
                XCTAssertEqual(persisted, self.new)
                stored.remove(reference)
            })
        XCTAssertTrue(stored.contains(old))
        persisted = new // NetworkExtension has confirmed the new reference.
        XCTAssertTrue(transaction.finish(error: nil))
        XCTAssertEqual(stored, Set([new]))
        XCTAssertFalse(transaction.finish(error: failure))
        XCTAssertEqual(stored, Set([new]))
    }

    func testFailedRemovalKeepsSecret() async {
        var stored = Set([old])
        let transaction = ConfigurationTransaction(oldReference: old, newReference: nil,
            restore: {}, delete: { stored.remove($0) })
        XCTAssertTrue(transaction.finish(error: failure))
        XCTAssertEqual(stored, Set([old]))
    }

    func testConfirmedRemovalDeletesSecret() async {
        var stored = Set([old])
        let transaction = ConfigurationTransaction(oldReference: old, newReference: nil,
            restore: {}, delete: { stored.remove($0) })
        XCTAssertEqual(stored, Set([old]))
        XCTAssertTrue(transaction.finish(error: nil))
        XCTAssertTrue(stored.isEmpty)
    }

    func testFailedAddRemovesOnlyUnpersistedSecret() async {
        var stored = Set([old, new])
        let transaction = ConfigurationTransaction(oldReference: nil, newReference: new,
            restore: {}, delete: { stored.remove($0) })
        XCTAssertTrue(transaction.finish(error: failure))
        XCTAssertEqual(stored, Set([old]))
    }

    func testUnchangedReferenceIsNeverDeleted() async {
        for error in [nil, failure] {
            let transaction = ConfigurationTransaction(oldReference: old, newReference: old,
                restore: {}, delete: { _ in XCTFail("An unchanged reference was deleted") })
            XCTAssertTrue(transaction.finish(error: error))
        }
    }

    func testFailedInlineMigrationRestoresOnlyReadableCopy() async {
        var inlineConfiguration: String? = nil // staged migration removed it
        var stored = Set([new])
        let transaction = ConfigurationTransaction(oldReference: nil, newReference: new,
            restore: { inlineConfiguration = "old inline config" },
            delete: { stored.remove($0) })
        XCTAssertTrue(transaction.finish(error: failure))
        XCTAssertEqual(inlineConfiguration, "old inline config")
        XCTAssertTrue(stored.isEmpty)
    }

    func testCanonicalReferenceMigrationNeverDeletesSharedItem() async {
        // Old iOS reference and canonical reference point at the same item;
        // neither is a newly created Keychain generation.
        var reference = new
        let transaction = ConfigurationTransaction(oldReference: nil, newReference: nil,
            restore: { reference = self.old },
            delete: { _ in XCTFail("Canonicalization deleted the shared item") })
        XCTAssertTrue(transaction.finish(error: failure))
        XCTAssertEqual(reference, old)
    }
}
