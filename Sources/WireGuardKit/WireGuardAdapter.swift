// SPDX-License-Identifier: MIT
// Copyright © 2018-2023 WireGuard LLC. All Rights Reserved.

import Foundation
import NetworkExtension

#if SWIFT_PACKAGE
import WireGuardKitGo
import WireGuardKitC
#endif

public enum WireGuardAdapterError: Error {
    case cannotLocateTunnelFileDescriptor
    case invalidState
    case dnsResolution([DNSResolutionError])
    case setNetworkSettings(Error)
    /// The outstanding system request cannot be cancelled; this adapter cannot restart.
    case networkSettingsTimedOut
    case providerUnavailable
    case startWireGuardBackend(Int32)
    case updateWireGuardBackend(Int64)
    case unexpected(Error)
}

public enum WireGuardAdapterState: Sendable {
    case stopped, starting, running, updating, stopping, paused, failed
}

/// Callback submissions execute in FIFO order, including asynchronous work.
/// All mutable tunnel state belongs to the lifecycle actor.
public final class WireGuardAdapter: Sendable {
    public typealias LogHandler = @Sendable (WireGuardLogLevel, String) -> Void
    private let operations: AsyncOperationQueue
    private let lifecycle: TunnelLifecycle
    private let loggerID: UUID

    public init(with provider: NEPacketTunnelProvider, logHandler: @escaping LogHandler) {
        let bridge = NetworkExtensionBridge(provider)
        let dependencies = AdapterDependencies(
            applySettings: { generator, completion in bridge.apply(generator, completion: completion) },
            reasserting: { bridge.setReasserting($0) },
            cancelTunnel: { bridge.cancel($0) },
            start: { configuration in
                guard let fd = Self.tunnelFileDescriptor else { throw WireGuardAdapterError.cannotLocateTunnelFileDescriptor }
                let handle = wgTurnOn(configuration, fd)
                guard handle >= 0 else { throw WireGuardAdapterError.startWireGuardBackend(handle) }
                #if os(iOS)
                wgDisableSomeRoamingForBrokenMobileSemantics(handle)
                #endif
                return handle
            },
            stop: { wgTurnOff($0) },
            update: { wgSetConfig($0, $1) },
            bumpSockets: { wgBumpSockets($0) },
            disableRoaming: { handle in
                #if os(iOS)
                wgDisableSomeRoamingForBrokenMobileSemantics(handle)
                #endif
            },
            runtimeConfiguration: { handle in
                guard let value = wgGetConfig(handle) else { return nil }
                defer { free(value) }
                return String(cString: value)
            })
        self.operations = AsyncOperationQueue()
        self.lifecycle = TunnelLifecycle(dependencies: dependencies, operations: operations, log: logHandler)
        self.loggerID = UUID()
        BackendLog.install(id: loggerID, handler: logHandler)
    }

    init(dependencies: AdapterDependencies, logHandler: @escaping LogHandler = { _, _ in }) {
        operations = AsyncOperationQueue()
        lifecycle = TunnelLifecycle(dependencies: dependencies, operations: operations, log: logHandler)
        loggerID = UUID()
    }

    deinit {
        let lifecycle = lifecycle
        operations.submit { await lifecycle.shutdown() }
        operations.finish()
        BackendLog.remove(id: loggerID)
    }

    public var lifecycleState: WireGuardAdapterState { get async { await lifecycle.phase } }

    public func start(tunnelConfiguration: TunnelConfiguration, completionHandler: @escaping @Sendable (WireGuardAdapterError?) -> Void) {
        let lifecycle = lifecycle
        operations.submit { completionHandler(await lifecycle.start(tunnelConfiguration)) }
    }

    public func update(tunnelConfiguration: TunnelConfiguration, completionHandler: @escaping @Sendable (WireGuardAdapterError?) -> Void) {
        let lifecycle = lifecycle
        operations.submit { completionHandler(await lifecycle.update(tunnelConfiguration)) }
    }

    public func stop(completionHandler: @escaping @Sendable (WireGuardAdapterError?) -> Void) {
        let lifecycle = lifecycle
        operations.submit { completionHandler(await lifecycle.stop()) }
    }

    public func getRuntimeConfiguration(completionHandler: @escaping @Sendable (String?) -> Void) {
        let lifecycle = lifecycle
        operations.submit { completionHandler(await lifecycle.runtimeConfiguration()) }
    }

    /// Tunnel device file descriptor.
    private static var tunnelFileDescriptor: Int32? {
        var ctlInfo = ctl_info()
        withUnsafeMutablePointer(to: &ctlInfo.ctl_name) {
            $0.withMemoryRebound(to: CChar.self, capacity: MemoryLayout.size(ofValue: $0.pointee)) {
                _ = strcpy($0, "com.apple.net.utun_control")
            }
        }
        for fd: Int32 in 0...1024 {
            var addr = sockaddr_ctl()
            var ret: Int32 = -1
            var len = socklen_t(MemoryLayout.size(ofValue: addr))
            withUnsafeMutablePointer(to: &addr) {
                $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                    ret = getpeername(fd, $0, &len)
                }
            }
            if ret != 0 || addr.sc_family != AF_SYSTEM {
                continue
            }
            if ctlInfo.ctl_id == 0 {
                ret = ioctl(fd, CTLIOCGINFO, &ctlInfo)
                if ret != 0 {
                    continue
                }
            }
            if addr.sc_id == ctlInfo.ctl_id {
                return fd
            }
        }
        return nil
    }

    /// Returns a WireGuard version.
    static var backendVersion: String {
        guard let ver = wgVersion() else { return "unknown" }
        let str = String(cString: ver)
        free(UnsafeMutableRawPointer(mutating: ver))
        return str
    }

    /// Returns the tunnel device interface name, or nil on error.
    /// - Returns: String.
    public var interfaceName: String? {
        guard let tunnelFileDescriptor = Self.tunnelFileDescriptor else { return nil }

        var buffer = [UInt8](repeating: 0, count: Int(IFNAMSIZ))

        return buffer.withUnsafeMutableBufferPointer { mutableBufferPointer in
            guard let baseAddress = mutableBufferPointer.baseAddress else { return nil }

            var ifnameSize = socklen_t(IFNAMSIZ)
            let result = getsockopt(
                tunnelFileDescriptor,
                2 /* SYSPROTO_CONTROL */,
                2 /* UTUN_OPT_IFNAME */,
                baseAddress,
                &ifnameSize)

            if result == 0 {
                return String(cString: baseAddress)
            } else {
                return nil
            }
        }
    }

}

/// The Objective-C provider cannot conform to Sendable. This narrow bridge
/// only calls NetworkExtension APIs from the serialized lifecycle; its weak
/// reference never escapes and framework completions carry only Error values.
private final class NetworkExtensionBridge: @unchecked Sendable {
    private weak var provider: NEPacketTunnelProvider?
    init(_ provider: NEPacketTunnelProvider) { self.provider = provider }
    func apply(_ generator: PacketTunnelSettingsGenerator, completion: @escaping @Sendable (Error?) -> Void) {
        guard let provider else { completion(WireGuardAdapterError.providerUnavailable); return }
        provider.setTunnelNetworkSettings(generator.generateNetworkSettings(), completionHandler: completion)
    }
    func setReasserting(_ value: Bool) { provider?.reasserting = value }
    func cancel(_ error: Error) { provider?.cancelTunnelWithError(error) }
}

/// Go keeps one process-wide C logger. Its callback is installed once and never
/// points at a Swift object's unmanaged address. In-flight logs retain a safe
/// closure snapshot even while an adapter is being destroyed.
private enum BackendLog {
    struct Registration: Sendable { let id: UUID; let handler: WireGuardAdapter.LogHandler }
    static let current = LockedValue<Registration?>(nil)
    static let registerCallback: Void = {
        wgSetLogger(nil) { _, level, message in
            guard let message, let handler = current.withLock({ $0?.handler }) else { return }
            handler(WireGuardLogLevel(rawValue: level) ?? .verbose, String(cString: message).trimmingCharacters(in: .newlines))
        }
    }()
    static func install(id: UUID, handler: @escaping WireGuardAdapter.LogHandler) {
        _ = registerCallback
        current.withLock { $0 = Registration(id: id, handler: handler) }
    }
    static func remove(id: UUID) {
        current.withLock { if $0?.id == id { $0 = nil } }
    }
}

public enum WireGuardLogLevel: Int32, Sendable { case verbose = 0, error = 1 }
