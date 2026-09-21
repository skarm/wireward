# Wireward for iOS and macOS

Wireward is a fork of [WireGuard/wireguard-apple](https://github.com/WireGuard/wireguard-apple), the [WireGuard](https://www.wireguard.com/) client for iOS and macOS.

The official upstream repository is [git.zx2c4.com/wireguard-apple](https://git.zx2c4.com/wireguard-apple). The GitHub mirror is also used to review community contributions.

This project contains applications for iOS and macOS, along with shared components. The `WireGuardKit` package name and existing application and Network Extension target names are retained.

## Building

Build with stable Xcode 27.0 (27A266a), Swift 6 language mode (Swift tools 6.4), and Go 1.27.1. Minimum deployment targets are macOS 13 and iOS 15. The iOS and macOS applications and Network Extensions have been built as unsigned Debug arm64 binaries. Real-device tunnel validation is pending.

- Clone this repo:

```
$ git clone https://github.com/skarm/wireward.git
$ cd wireward
```

- Copy and populate the developer configuration file:

```
$ cp Sources/WireGuardApp/Config/Developer.xcconfig.template Sources/WireGuardApp/Config/Developer.xcconfig
$ vim Sources/WireGuardApp/Config/Developer.xcconfig
```

- Install SwiftLint:

```
$ brew install swiftlint
```

- Install [Go 1.27.1](https://go.dev/dl/#go1.27.1), then build the Go bridge from the repository root:

```sh
make -C Sources/WireGuardKitGo xcframework GO=/absolute/path/to/go
```

This command produces `Artifacts/WireGuardKitGo.xcframework`, `Artifacts/wireguard-go-version.h`, and `Artifacts/WireGuardKitGo.build.json`. Xcode and Swift Package Manager consume this local artifact. Re-run the command after changing the bridge sources, dependencies, runtime patch, or toolchain; cached slices are reused when their inputs match.

The XCFramework contains macOS arm64/x86_64, iOS arm64, and iOS Simulator arm64/x86_64. Deployment targets default to macOS 13 and iOS 15. To build only Apple silicon slices, pass `MACOS_ARCHS=arm64 SIMULATOR_ARCHS=arm64`. `MACOS_DEPLOYMENT_TARGET` and `IOS_DEPLOYMENT_TARGET` can raise the minimum versions. `CACHE_DIR` and `OUTPUT_DIR` override the default locations; the app and package expect `Artifacts` unless you update their references.

The driver requires the versions recorded in `.go-version` and `.xcode-version`, uses the selected Xcode's SDKs and Clang, and disables automatic Go toolchain switching. `GO` must be an absolute path; it defaults to the official install location `/usr/local/go/bin/go`. No Homebrew PATH is added. `DEVELOPER_DIR` can select an Xcode installation.

The cache key includes toolchain and SDK identities, architectures, deployment targets, source files, dependency manifests, build code, and the runtime patch. Cached archives are checked by SHA-256. The Darwin clock patch is applied to three runtime source files through a Go overlay; the installed GOROOT is left intact. The build manifest records these inputs and the archive checksums. Generated artifacts and caches are ignored by Git.

- Open project in Xcode:

```
$ open WireGuard.xcodeproj
```

- Select the `WireGuardiOS` or `WireGuardmacOS` scheme, configure signing for the application and Network Extension targets, and build.

## Tests

Build the XCFramework first, then run the Swift package and build-driver tests:

```sh
make -C Sources/WireGuardKitGo xcframework GO=/absolute/path/to/go
make -C Sources/WireGuardKitGo test GO=/absolute/path/to/go
swift test --enable-xctest
```

The C regression tests check key generation with assertions disabled, RNG failure, and kernel structure layouts against the macOS SDK:

```sh
mkdir -p build/tests
xcrun clang -DNDEBUG -DCCRandomGenerateBytes=wg_test_random -fsanitize=address,undefined -I Sources/WireGuardKitC Tests/WireGuardKitCTests/key-generation.c Sources/WireGuardKitC/x25519.c -o build/tests/key-generation
build/tests/key-generation
xcrun clang -std=c11 -Wall -Wextra -Werror -I Sources/WireGuardKitC Tests/WireGuardKitCTests/darwin-layout.c -o build/tests/darwin-layout
build/tests/darwin-layout
```

Add `--sanitize thread` to the Swift test command to check synchronization. Run upstream Go tests:

```sh
cd Sources/WireGuardKitGo
GOTOOLCHAIN=local go mod download github.com/google/btree # Upstream netstack test checksum
GOTOOLCHAIN=local go test -race ./... golang.zx2c4.com/wireguard/...
```

From `Sources/WireGuardKitGo`, run `go test -run '^$' -fuzz '^FuzzUAPI$' -fuzztime=30s` and `go test -run '^$' -fuzz '^FuzzStackCapture$' -fuzztime=30s` for bounded parser and diagnostic fuzz checks. UAPI tests use an in-memory TUN and do not establish a real VPN.

## Backend dependencies

The backend is pinned to `wireguard-go/master` commit `ecfc5a8d54462e18e13c72173e2623d16d8e25a0` (2026-05-22). Dependencies were checked on 2026-09-21. `Sources/WireGuardKitGo/go.mod` records WireGuard, `x/sys`, and six indirect dependencies: `x/crypto`, `x/exp`, `x/net`, `x/time`, Wintun, and gVisor.

For optional netstack support, gVisor uses the [Go-compatible branch](https://github.com/google/gvisor#using-go-get), pinned to `501da953ee3803d88004cba68c7cc85098c75dfb`; its Bazel `master` cannot be built with standard Go tooling.

Update these dependencies explicitly, resolve WireGuard `master` and gVisor `go` to exact pseudo-versions, and run the Go tests and both application builds. Keep the manifest and checksums minimal with `go mod tidy`. Release builds use the recorded versions without querying branches.

## Swift 6 compatibility

`TunnelConfiguration` is now a value type: edit a `var` copy and pass it back explicitly. Keys are immutable final `Sendable` types; `BaseKey` is a protocol. Adapter callbacks are `@Sendable`; app tunnel management and UI delegates are isolated to `MainActor`. Internal target names and bundle identifiers are unchanged.

Generate keys with `try PrivateKey()` and handle RNG failure. Generation runs in both Debug and Release; failures never produce a key. Hex/base64 initializers reject trailing content, including embedded NUL suffixes.

`WireGuardAdapter` is a checked `Sendable` type. An actor owns its lifecycle state, and a single asynchronous operation queue preserves submission order across DNS resolution and NetworkExtension callbacks. Read `await adapter.lifecycleState` for the current phase. The existing callback API remains available; callbacks do not run on the main actor unless the caller explicitly transfers them there.

Applying network settings has a five-second deadline. A timeout fails the operation instead of starting a backend with unconfirmed routes. Because NetworkExtension cannot cancel that request, the timed-out adapter refuses further starts; recovery requires ending the provider session. Duplicate or late callbacks cannot resume an operation twice. A backend configuration error during an update stops the tunnel instead of reporting success with inconsistent routes and peers; transactional rollback remains future work.

The package's `WireGuardKit` target treats warnings as errors. The application targets still have legacy UI and Keychain deprecation warnings; a project-wide warnings-as-errors gate is pending. Lifecycle unit tests cover callback races, operation ordering, timeouts, backend errors, stale network events, mobile pause/resume, and cleanup. These tests use platform substitutes and do not replace signed tunnel and device testing.

## Go bridge ABI

`Sources/WireGuardKitGo/wireguard.h` defines ABI version 2, including nullability, ownership, threading, and error contracts. Rebuild the XCFramework and all consumers together. Handles use fixed-width integers and are never reused within a process. Device operations return zero on success or a negative Darwin errno; start returns a nonnegative handle. `wgBumpSockets` reports that a retry worker was accepted, with final asynchronous failure reported through logging. Stop cancels and joins that worker.

`wgGetConfig(handle, &settings)` now returns a status separately from its owned string. Release it and the result of `wgVersion()` with `wgFreeString`. Swift callers can use `getRuntimeConfigurationResult` to receive the backend error; the existing `getRuntimeConfiguration` maps errors to `nil`. Logger callbacks must not synchronously call back into the bridge. Full configuration snapshots always replace peers, including when the new list is empty.

## WireGuardKit integration

Clone [skarm/wireward](https://github.com/skarm/wireward), build the XCFramework with the command above, and add this checkout as a **local Swift package** in Xcode. Link the `WireGuardKit` product to your application and Network Extension targets. The package's binary target supplies the Go library and headers for the selected platform; no External Build System target or manual library search path is required.

The generated XCFramework is not checked into Git. Adding the GitHub URL directly as a remote package requires a published binary artifact and checksum, which this repository does not provide yet.

## MIT License

Permission is hereby granted, free of charge, to any person obtaining a copy of
this software and associated documentation files (the "Software"), to deal in
the Software without restriction, including without limitation the rights to
use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies
of the Software, and to permit persons to whom the Software is furnished to do
so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
