// SPDX-License-Identifier: MIT

import Foundation
import XCTest
@testable import WireGuardKit

private final class FakeAdapter: Sendable {
    struct Storage: Sendable {
        var events: [String] = []
        var callbacks: [@Sendable (Error?) -> Void] = []
        var paths: [@Sendable (AdapterPathUpdate) -> Void] = []
        var applied = 0
        var updateCode: Int64 = 0
    }
    let storage = LockedValue(Storage())
    let requested: [XCTestExpectation]
    let closed = XCTestExpectation(description: "backend closed")
    init(requests: Int = 1) {
        requested = (0..<requests).map { XCTestExpectation(description: "settings request \($0)") }
    }
    var events: [String] { storage.withLock { $0.events } }
    func event(_ value: String) { storage.withLock { $0.events.append(value) } }
    func takeCallback() -> @Sendable (Error?) -> Void { storage.withLock { $0.callbacks.removeFirst() } }
    func dependencies(timeout: UInt64 = 2_000_000_000) -> AdapterDependencies {
        var dependencies = AdapterDependencies(
            applySettings: { [self] _, completion in
                let index = storage.withLock { state in
                    state.events.append("settings")
                    state.callbacks.append(completion)
                    defer { state.applied += 1 }
                    return state.applied
                }
                if index < requested.count { requested[index].fulfill() }
            },
            reasserting: { [self] value in event("reasserting:\(value)") },
            cancelTunnel: { [self] _ in event("cancel") },
            start: { [self] _ in event("start"); return 42 },
            stop: { [self] _ in event("stop"); closed.fulfill() },
            update: { [self] _, _ in event("update"); return storage.withLock { $0.updateCode } },
            bumpSockets: { [self] _ in event("bump") },
            disableRoaming: { [self] _ in event("roaming") },
            runtimeConfiguration: { _ in "test-config" })
        dependencies.timeoutNanoseconds = timeout
        dependencies.monitor = { [self] callback in
            storage.withLock { $0.paths.append(callback) }
            return { [self] in event("monitor-cancel") }
        }
        return dependencies
    }
}

@MainActor
final class AdapterLifecycleTests: XCTestCase {
    private func configuration() throws -> TunnelConfiguration {
        let key = try XCTUnwrap(PrivateKey(rawValue: Data(repeating: 1, count: 32)))
        return TunnelConfiguration(name: "test", interface: InterfaceConfiguration(privateKey: key), peers: [])
    }

    func testStartUpdateStopAreFIFOAcrossSuspension() async throws {
        let fake = FakeAdapter(requests: 2)
        let adapter = WireGuardAdapter(dependencies: fake.dependencies())
        let started = expectation(description: "started")
        let updated = expectation(description: "updated")
        let stopped = expectation(description: "stopped")
        let config = try configuration()
        adapter.start(tunnelConfiguration: config) { error in XCTAssertNil(error); started.fulfill() }
        adapter.update(tunnelConfiguration: config) { error in XCTAssertNil(error); updated.fulfill() }
        adapter.stop { error in XCTAssertNil(error); stopped.fulfill() }
        await fulfillment(of: [fake.requested[0]], timeout: 1)
        let starting = await adapter.lifecycleState
        XCTAssertEqual(starting, .starting)
        XCTAssertEqual(fake.events, ["settings"])
        fake.takeCallback()(nil)
        await fulfillment(of: [started, fake.requested[1]], timeout: 1)
        let updating = await adapter.lifecycleState
        XCTAssertEqual(updating, .updating)
        XCTAssertFalse(fake.events.contains("stop"))
        fake.takeCallback()(nil)
        await fulfillment(of: [updated, stopped], timeout: 1)
        let final = await adapter.lifecycleState
        XCTAssertEqual(final, .stopped)
        XCTAssertEqual(fake.events, ["settings", "start", "reasserting:true", "settings", "update", "roaming", "reasserting:false", "monitor-cancel", "stop"])
    }

    func testTimeoutDoesNotStartBackendOrAllowLateSettingsToRaceRestart() async throws {
        let fake = FakeAdapter()
        let adapter = WireGuardAdapter(dependencies: fake.dependencies(timeout: 30_000_000))
        let done = expectation(description: "timeout")
        adapter.start(tunnelConfiguration: try configuration()) { error in
            guard case .networkSettingsTimedOut? = error else { XCTFail("expected timeout"); done.fulfill(); return }
            done.fulfill()
        }
        await fulfillment(of: [fake.requested[0], done], timeout: 1)
        let failed = await adapter.lifecycleState
        XCTAssertEqual(failed, .failed)
        XCTAssertEqual(fake.events, ["settings"])
        let late = fake.takeCallback()
        late(nil)
        late(NSError(domain: "test", code: 1))
        let stopped = expectation(description: "stop failed adapter")
        adapter.stop { error in XCTAssertNil(error); stopped.fulfill() }
        await fulfillment(of: [stopped], timeout: 1)
        let retried = expectation(description: "unsafe retry refused")
        adapter.start(tunnelConfiguration: try configuration()) { error in
            guard case .networkSettingsTimedOut? = error else { XCTFail("unsafe retry accepted"); retried.fulfill(); return }
            retried.fulfill()
        }
        await fulfillment(of: [retried], timeout: 1)
        XCTAssertEqual(fake.events, ["settings"])
    }

    func testDuplicateCallbackCompletesOnce() async throws {
        let fake = FakeAdapter()
        let adapter = WireGuardAdapter(dependencies: fake.dependencies())
        let done = expectation(description: "started once")
        done.assertForOverFulfill = true
        adapter.start(tunnelConfiguration: try configuration()) { error in XCTAssertNil(error); done.fulfill() }
        await fulfillment(of: [fake.requested[0]], timeout: 1)
        let callback = fake.takeCallback()
        callback(nil)
        callback(NSError(domain: "late", code: 1))
        await fulfillment(of: [done], timeout: 1)
        XCTAssertEqual(fake.events, ["settings", "start"])
    }

    func testBackendUpdateFailureStopsInsteadOfReportingSuccess() async throws {
        let fake = FakeAdapter(requests: 2)
        fake.storage.withLock { $0.updateCode = -5 }
        let adapter = WireGuardAdapter(dependencies: fake.dependencies())
        let started = expectation(description: "start")
        adapter.start(tunnelConfiguration: try configuration()) { error in XCTAssertNil(error); started.fulfill() }
        await fulfillment(of: [fake.requested[0]], timeout: 1)
        fake.takeCallback()(nil)
        await fulfillment(of: [started], timeout: 1)
        let updated = expectation(description: "update failed")
        adapter.update(tunnelConfiguration: try configuration()) { error in
            guard case .updateWireGuardBackend(-5)? = error else { XCTFail("lost backend error"); updated.fulfill(); return }
            updated.fulfill()
        }
        await fulfillment(of: [fake.requested[1]], timeout: 1)
        fake.takeCallback()(nil)
        await fulfillment(of: [updated, fake.closed], timeout: 1)
        let phase = await adapter.lifecycleState
        XCTAssertEqual(phase, .failed)
        XCTAssertEqual(fake.events.filter { $0 == "stop" }.count, 1)
        XCTAssertTrue(fake.events.contains("cancel"))
        XCTAssertEqual(fake.events.last, "reasserting:false")
    }

    func testOldMonitorCannotAffectRestartedSession() async throws {
        let fake = FakeAdapter(requests: 2)
        let adapter = WireGuardAdapter(dependencies: fake.dependencies())
        let first = expectation(description: "first")
        adapter.start(tunnelConfiguration: try configuration()) { _ in first.fulfill() }
        await fulfillment(of: [fake.requested[0]], timeout: 1)
        fake.takeCallback()(nil)
        await fulfillment(of: [first], timeout: 1)
        let oldPath = fake.storage.withLock { $0.paths[0] }
        let stopped = expectation(description: "stop")
        adapter.stop { _ in stopped.fulfill() }
        await fulfillment(of: [stopped], timeout: 1)
        let second = expectation(description: "second")
        adapter.start(tunnelConfiguration: try configuration()) { error in XCTAssertNil(error); second.fulfill() }
        await fulfillment(of: [fake.requested[1]], timeout: 1)
        fake.takeCallback()(nil)
        await fulfillment(of: [second], timeout: 1)
        oldPath(AdapterPathUpdate(satisfiable: false, description: "stale"))
        let barrier = expectation(description: "path processed")
        adapter.getRuntimeConfiguration { config in XCTAssertEqual(config, "test-config"); barrier.fulfill() }
        await fulfillment(of: [barrier], timeout: 1)
        XCTAssertFalse(fake.events.contains("bump"))
        XCTAssertEqual(fake.events.filter { $0 == "stop" }.count, 1)
    }

    func testDeinitDrainsBackendCleanup() async throws {
        let fake = FakeAdapter()
        var adapter: WireGuardAdapter? = WireGuardAdapter(dependencies: fake.dependencies())
        let started = expectation(description: "start")
        adapter?.start(tunnelConfiguration: try configuration()) { error in XCTAssertNil(error); started.fulfill() }
        await fulfillment(of: [fake.requested[0]], timeout: 1)
        fake.takeCallback()(nil)
        await fulfillment(of: [started], timeout: 1)
        adapter = nil
        await fulfillment(of: [fake.closed], timeout: 1)
        XCTAssertEqual(fake.events.suffix(2), ["monitor-cancel", "stop"])
    }

    func testUpdateTimeoutStopsBackendAndClearsReasserting() async throws {
        let fake = FakeAdapter(requests: 2)
        let adapter = WireGuardAdapter(dependencies: fake.dependencies(timeout: 100_000_000))
        let started = expectation(description: "started")
        adapter.start(tunnelConfiguration: try configuration()) { error in XCTAssertNil(error); started.fulfill() }
        await fulfillment(of: [fake.requested[0]], timeout: 1)
        fake.takeCallback()(nil)
        await fulfillment(of: [started], timeout: 1)
        let updated = expectation(description: "update timed out")
        adapter.update(tunnelConfiguration: try configuration()) { error in
            guard case .networkSettingsTimedOut? = error else { XCTFail("expected timeout"); updated.fulfill(); return }
            updated.fulfill()
        }
        await fulfillment(of: [updated, fake.closed], timeout: 1)
        fake.takeCallback()(nil)
        let phase = await adapter.lifecycleState
        XCTAssertEqual(phase, .failed)
        XCTAssertFalse(fake.events.contains("update"))
        XCTAssertEqual(fake.events.suffix(3), ["stop", "cancel", "reasserting:false"])
    }

    func testSettingsErrorDoesNotStartBackendAndAllowsRetry() async throws {
        let fake = FakeAdapter(requests: 2)
        let adapter = WireGuardAdapter(dependencies: fake.dependencies())
        let rejected = expectation(description: "settings rejected")
        adapter.start(tunnelConfiguration: try configuration()) { error in
            guard case .setNetworkSettings(let underlying)? = error else { XCTFail("lost settings error"); rejected.fulfill(); return }
            XCTAssertEqual((underlying as NSError).code, 17)
            rejected.fulfill()
        }
        await fulfillment(of: [fake.requested[0]], timeout: 1)
        fake.takeCallback()(NSError(domain: "test", code: 17))
        await fulfillment(of: [rejected], timeout: 1)
        XCTAssertEqual(fake.events, ["settings"])
        let restarted = expectation(description: "retry succeeded")
        adapter.start(tunnelConfiguration: try configuration()) { error in XCTAssertNil(error); restarted.fulfill() }
        await fulfillment(of: [fake.requested[1]], timeout: 1)
        fake.takeCallback()(nil)
        await fulfillment(of: [restarted], timeout: 1)
        let phase = await adapter.lifecycleState
        XCTAssertEqual(phase, .running)
    }

    func testMobileNetworkLossPausesAndResumeFinishesBeforeQueuedStop() async throws {
        let fake = FakeAdapter(requests: 2)
        var dependencies = fake.dependencies()
        dependencies.pathBehavior = .pauseAndReconnect
        let adapter = WireGuardAdapter(dependencies: dependencies)
        let started = expectation(description: "started")
        adapter.start(tunnelConfiguration: try configuration()) { error in XCTAssertNil(error); started.fulfill() }
        await fulfillment(of: [fake.requested[0]], timeout: 1)
        fake.takeCallback()(nil)
        await fulfillment(of: [started], timeout: 1)
        let path = fake.storage.withLock { $0.paths[0] }
        path(AdapterPathUpdate(satisfiable: false, description: "offline"))
        let paused = expectation(description: "paused")
        adapter.getRuntimeConfiguration { config in XCTAssertNil(config); paused.fulfill() }
        await fulfillment(of: [paused], timeout: 1)
        let phase = await adapter.lifecycleState
        XCTAssertEqual(phase, .paused)
        XCTAssertEqual(fake.events.filter { $0 == "stop" }.count, 1)
        path(AdapterPathUpdate(satisfiable: true, description: "online"))
        let stopped = expectation(description: "stop waits for resume")
        adapter.stop { error in XCTAssertNil(error); stopped.fulfill() }
        await fulfillment(of: [fake.requested[1]], timeout: 1)
        XCTAssertFalse(fake.events.contains("monitor-cancel"))
        fake.takeCallback()(nil)
        await fulfillment(of: [stopped], timeout: 1)
        XCTAssertEqual(fake.events.suffix(4), ["settings", "start", "monitor-cancel", "stop"])
        let final = await adapter.lifecycleState
        XCTAssertEqual(final, .stopped)
    }

    func testCallbackWaitCancellationWinsOverLateCompletion() async throws {
        let started = expectation(description: "callback installed")
        let callback = LockedValue<(@Sendable (Error?) -> Void)?>(nil)
        let task = Task {
            try await CallbackResult.wait(timeoutNanoseconds: 5_000_000_000) { completion in
                callback.withLock { $0 = completion }
                started.fulfill()
            }
        }
        await fulfillment(of: [started], timeout: 1)
        task.cancel()
        do { try await task.value; XCTFail("cancellation ignored") } catch is CancellationError { } catch { XCTFail("wrong error: \(error)") }
        callback.withLock { $0 }?(nil)
    }
}
