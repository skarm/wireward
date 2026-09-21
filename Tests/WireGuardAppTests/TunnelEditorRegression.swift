// SPDX-License-Identifier: MIT

import AppKit
import NetworkExtension

/// Standalone entry point for run-editor-tests.py. Uses the actual app classes
/// with in-memory profiles; the normal AppDelegate is never launched.
@main
struct TunnelEditorRegression {
    @MainActor
    static func main() throws {
        _ = NSApplication.shared
        let provider = NETunnelProviderManager()
        provider.localizedDescription = "Unreadable profile"
        let proto = NETunnelProviderProtocol()
        proto.providerConfiguration = ["UID": getuid()]
        provider.protocolConfiguration = proto
        let manager = TunnelsManager(tunnelProviders: [provider])
        let tunnel = manager.tunnel(at: 0)

        // No password reference and no inline fallback: configuration is nil.
        let unavailable: TunnelEditViewController? = TunnelEditViewController(tunnelsManager: manager, tunnel: tunnel)
        check(unavailable == nil, "An unreadable profile opened an editor")
        check(manager.numberOfTunnels() == 1, "The unreadable profile was deleted")
        print("PASS: unreadable profile is retained and cannot open an empty editor")

        // Retry after the configuration becomes readable. Inline data keeps
        // the test independent of Keychain permissions and signing.
        let key = PrivateKey(rawValue: Data(repeating: 1, count: 32))!
        let configuration = TunnelConfiguration(name: tunnel.name, interface: InterfaceConfiguration(privateKey: key), peers: [])
        proto.providerConfiguration = ["UID": getuid(), "WgQuickConfig": configuration.asWgQuickConfig()]
        provider.protocolConfiguration = proto
        let retry: TunnelEditViewController? = TunnelEditViewController(tunnelsManager: manager, tunnel: tunnel)
        check(retry != nil, "Retry did not accept a readable configuration")

        // Access can disappear between construction and loadView. The editor
        // must keep the accepted snapshot rather than read and unwrap nil.
        proto.providerConfiguration = ["UID": getuid()]
        provider.protocolConfiguration = proto
        provider.cacheTunnelConfiguration(nil)
        check(tunnel.tunnelConfiguration == nil, "The test did not remove configuration access")
        retry!.loadView()
        check(retry!.textView.string == configuration.asWgQuickConfig(), "The editor lost its accepted snapshot")
        check(retry!.publicKeyRow.value == key.publicKey.base64Key, "The editor generated a replacement key")
        print("PASS: retry succeeds and editing survives loss of configuration access")

        let fresh: TunnelEditViewController? = TunnelEditViewController(tunnelsManager: manager, tunnel: nil)
        check(fresh != nil, "Creating a new tunnel was rejected")
        fresh!.loadView()
        let generated = try TunnelConfiguration(fromWgQuickConfig: fresh!.textView.string)
        check(generated.interface.privateKey.publicKey.base64Key == fresh!.publicKeyRow.value, "New tunnel keys do not match")
        print("PASS: new tunnel creation still generates a valid key pair")
    }

    private static func check(_ condition: Bool, _ message: String) {
        guard condition else {
            print("FAIL: \(message)")
            exit(1)
        }
    }
}
