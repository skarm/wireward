// SPDX-License-Identifier: MIT

import Foundation
import Network

struct AdapterPathUpdate: Sendable {
    let satisfiable: Bool
    let description: String
}

/// Only this internal seam replaces platform calls in lifecycle regression tests.
struct AdapterDependencies: Sendable {
    enum PathBehavior: Sendable { case bumpSockets, pauseAndReconnect }
    #if os(macOS)
    var pathBehavior: PathBehavior = .bumpSockets
    #else
    var pathBehavior: PathBehavior = .pauseAndReconnect
    #endif
    var applySettings: @Sendable (PacketTunnelSettingsGenerator, @escaping @Sendable (Error?) -> Void) -> Void
    var reasserting: @Sendable (Bool) -> Void
    var cancelTunnel: @Sendable (Error) -> Void
    var start: @Sendable (String) throws -> Int32
    var stop: @Sendable (Int32) throws -> Void
    var update: @Sendable (Int32, String) -> Int64
    var bumpSockets: @Sendable (Int32) throws -> Void
    var disableRoaming: @Sendable (Int32) throws -> Void
    var runtimeConfiguration: @Sendable (Int32) throws -> String
    var timeoutNanoseconds: UInt64 = 5_000_000_000
    var resolveConfiguration: @Sendable (PacketTunnelSettingsGenerator, Bool) async -> (String, [EndpointResolutionResult?]) = { generator, endpointsOnly in
        await DNSResolver.run { endpointsOnly ? generator.endpointUapiConfiguration() : generator.uapiConfiguration() }
    }
    var monitor: @Sendable (@escaping @Sendable (AdapterPathUpdate) -> Void) -> (@Sendable () -> Void) = { callback in
        let monitor = NWPathMonitor()
        monitor.pathUpdateHandler = { path in
            callback(AdapterPathUpdate(satisfiable: path.status != .unsatisfied,
                                       description: "\(path.status), interfaces: \(path.availableInterfaces)"))
        }
        monitor.start(queue: DispatchQueue(label: "WireGuardPathMonitor"))
        return { monitor.cancel() }
    }
}

actor TunnelLifecycle {
    private(set) var phase: WireGuardAdapterState = .stopped
    private let dependencies: AdapterDependencies
    private let operations: AsyncOperationQueue
    private let log: WireGuardAdapter.LogHandler
    private var handle: Int32?
    private var generator: PacketTunnelSettingsGenerator?
    private var monitorID: UUID?
    private var cancelMonitor: (@Sendable () -> Void)?
    // A timed-out NE request cannot be cancelled. Never issue another request
    // from this adapter: a late callback could otherwise overwrite newer routes.
    private var settingsUncertain = false

    init(dependencies: AdapterDependencies, operations: AsyncOperationQueue, log: @escaping WireGuardAdapter.LogHandler) {
        self.dependencies = dependencies
        self.operations = operations
        self.log = log
    }

    func start(_ configuration: TunnelConfiguration) async -> WireGuardAdapterError? {
        guard phase == .stopped || phase == .failed else { return .invalidState }
        guard !settingsUncertain else { return .networkSettingsTimedOut }
        phase = .starting
        do {
            let generator = try await makeGenerator(configuration)
            let config = try await prepareConfiguration(generator)
            try await apply(generator)
            handle = try dependencies.start(config)
            self.generator = generator
            phase = .running
            startMonitor()
            return nil
        } catch {
            fail()
            return adapterError(error)
        }
    }

    func stop() -> WireGuardAdapterError? {
        guard phase != .stopped else { return .invalidState }
        phase = .stopping
        return shutdown()
    }

    func shutdown() -> WireGuardAdapterError? {
        cancelMonitor?()
        cancelMonitor = nil
        monitorID = nil
        var failure: WireGuardAdapterError?
        if let handle {
            do { try dependencies.stop(handle) } catch {
                failure = adapterError(error)
                log(.error, "Backend shutdown failed: \(error)")
            }
        }
        handle = nil
        generator = nil
        phase = .stopped
        return failure
    }

    func update(_ configuration: TunnelConfiguration) async -> WireGuardAdapterError? {
        guard phase == .running || phase == .paused else { return .invalidState }
        let previousPhase = phase
        phase = .updating
        dependencies.reasserting(true)
        defer { dependencies.reasserting(false) }
        var settingsApplied = false
        var settingsAttempted = false
        do {
            let updated = try await makeGenerator(configuration)
            let config = try await prepareConfiguration(updated)
            settingsAttempted = true
            try await apply(updated)
            settingsApplied = true
            if let handle {
                let code = dependencies.update(handle, config)
                guard code == 0 else {
                    let error = WireGuardAdapterError.updateWireGuardBackend(code)
                    fail()
                    dependencies.cancelTunnel(error)
                    return error
                }
                try dependencies.disableRoaming(handle)
            }
            generator = updated
            phase = previousPhase
            return nil
        } catch {
            if settingsUncertain || settingsApplied {
                fail()
                dependencies.cancelTunnel(error)
            } else if settingsAttempted, let generator {
                // The system reported failure, but may have partially changed
                // routes/DNS. Restore the last confirmed settings before resuming.
                do {
                    try await apply(generator)
                    phase = previousPhase
                } catch {
                    log(.error, "Network settings rollback failed: \(error)")
                    fail()
                    dependencies.cancelTunnel(error)
                }
            } else {
                phase = previousPhase
            }
            return adapterError(error)
        }
    }

    func runtimeConfiguration() -> Result<String, WireGuardAdapterError> {
        guard phase == .running, let handle else { return .failure(.invalidState) }
        do { return .success(try dependencies.runtimeConfiguration(handle)) }
        catch { return .failure(adapterError(error)) }
    }

    private func apply(_ generator: PacketTunnelSettingsGenerator) async throws {
        guard !settingsUncertain else { throw WireGuardAdapterError.networkSettingsTimedOut }
        do {
            try await CallbackResult.wait(timeoutNanoseconds: dependencies.timeoutNanoseconds) { completion in
                dependencies.applySettings(generator, completion)
            }
        } catch WireGuardAdapterError.networkSettingsTimedOut {
            settingsUncertain = true
            throw WireGuardAdapterError.networkSettingsTimedOut
        } catch let error as WireGuardAdapterError {
            throw error
        } catch {
            throw WireGuardAdapterError.setNetworkSettings(error)
        }
    }

    private func makeGenerator(_ configuration: TunnelConfiguration) async throws -> PacketTunnelSettingsGenerator {
        let results = await DNSResolver.run { DNSResolver.resolveSync(endpoints: configuration.peers.map { $0.endpoint }) }
        var endpoints: [Endpoint?] = []
        var failures: [DNSResolutionError] = []
        for result in results {
            switch result {
            case .success(let endpoint): endpoints.append(endpoint)
            case .failure(let error): failures.append(error)
            case nil: endpoints.append(nil)
            }
        }
        guard failures.isEmpty else { throw WireGuardAdapterError.dnsResolution(failures) }
        return PacketTunnelSettingsGenerator(tunnelConfiguration: configuration, resolvedEndpoints: endpoints)
    }

    private func prepareConfiguration(_ generator: PacketTunnelSettingsGenerator, endpointsOnly: Bool = false) async throws -> String {
        let (config, results) = await dependencies.resolveConfiguration(generator, endpointsOnly)
        logResolution(results)
        let failures = results.compactMap { result -> DNSResolutionError? in
            if case .failure(let error) = result { return error }
            return nil
        }
        guard failures.isEmpty else { throw WireGuardAdapterError.dnsResolution(failures) }
        return config
    }

    private func startMonitor() {
        let id = UUID()
        monitorID = id
        let operations = operations
        cancelMonitor = dependencies.monitor { [weak self] update in
            operations.submit { [weak self] in await self?.pathChanged(update, id: id) }
        }
    }

    private func pathChanged(_ path: AdapterPathUpdate, id: UUID) async {
        guard monitorID == id else { return }
        log(.verbose, "Network change: \(path.description)")
        do {
            if dependencies.pathBehavior == .bumpSockets {
                if phase == .running, let handle { try dependencies.bumpSockets(handle) }
                return
            }
            guard let generator else { return }
            if phase == .running, let handle {
                if path.satisfiable {
                    do {
                        let config = try await prepareConfiguration(generator, endpointsOnly: true)
                        let code = dependencies.update(handle, config)
                        guard code == 0 else {
                            fail()
                            dependencies.cancelTunnel(WireGuardAdapterError.updateWireGuardBackend(code))
                            return
                        }
                    } catch WireGuardAdapterError.dnsResolution {
                        // Keep the previous endpoints, but still move the UDP
                        // sockets off the old path and send keepalives.
                        log(.error, "Keeping previous endpoints after DNS resolution failed during network change")
                    }
                    try dependencies.disableRoaming(handle)
                    try dependencies.bumpSockets(handle)
                } else {
                    try dependencies.stop(handle)
                    self.handle = nil
                    phase = .paused
                }
            } else if phase == .paused && path.satisfiable {
                phase = .starting
                do {
                    let config = try await prepareConfiguration(generator)
                    try await apply(generator)
                    handle = try dependencies.start(config)
                    phase = .running
                } catch {
                    log(.error, "Failed to resume backend: \(error.localizedDescription)")
                    if case .dnsResolution = adapterError(error) {
                        phase = .paused
                        return
                    }
                    fail()
                    dependencies.cancelTunnel(error)
                }
            }
        } catch {
            log(.error, "Network change failed: \(error)")
            fail()
            dependencies.cancelTunnel(error)
        }
    }

    private func fail() { _ = shutdown(); phase = .failed }
    private func adapterError(_ error: Error) -> WireGuardAdapterError { error as? WireGuardAdapterError ?? .unexpected(error) }

    private func logResolution(_ results: [EndpointResolutionResult?]) {
        for case .some(let result) in results {
            switch result {
            case .success((let source, let resolved)):
                log(.verbose, "DNS64: mapped \(source.host) to \(resolved.host)")
            case .failure(let error):
                log(.error, "Failed to resolve endpoint \(error.address): \(error.localizedDescription)")
            }
        }
    }
}
