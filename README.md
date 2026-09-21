# Wireward for iOS and macOS

Wireward is a fork of [WireGuard/wireguard-apple](https://github.com/WireGuard/wireguard-apple), the [WireGuard](https://www.wireguard.com/) client for iOS and macOS.

The official upstream repository is [git.zx2c4.com/wireguard-apple](https://git.zx2c4.com/wireguard-apple). The GitHub mirror is also used to review community contributions.

This project contains applications for iOS and macOS, along with shared components. The `WireGuardKit` package name and existing Xcode target names are retained.

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

- Install [Go 1.27.1](https://go.dev/dl/#go1.27.1). Set `GO` in `Developer.xcconfig` to the absolute path of its executable (for example, `GO = /opt/homebrew/bin/go`), or pass `GO=/absolute/path/to/go` to `xcodebuild` / `make`. The bridge rejects other Go versions and disables automatic toolchain switching.

- Open project in Xcode:

```
$ open WireGuard.xcodeproj
```

- Select the `WireGuardiOS` or `WireGuardmacOS` scheme, configure signing for the application and Network Extension targets, and build.

## Tests

On Apple silicon, build the macOS bridge and run the package tests:

```sh
xcodebuild -project WireGuard.xcodeproj -scheme WireGuardmacOS -configuration Debug \
  -sdk macosx -destination 'generic/platform=macOS' -derivedDataPath build/macos \
  CODE_SIGNING_ALLOWED=NO ARCHS=arm64 GO="$(command -v go)" build
swift test --enable-xctest -Xlinker -Lbuild/macos/Build/Products/Debug
```

Add `--sanitize thread` to the Swift test command to check synchronization. Run upstream Go tests with the manifest kept read-only:

```sh
cd Sources/WireGuardKitGo
GOTOOLCHAIN=local go mod download github.com/google/btree # Upstream netstack test checksum
GOTOOLCHAIN=local go test -mod=readonly -race ./... golang.zx2c4.com/wireguard/...
```

## Backend dependencies

The backend is pinned to `wireguard-go/master` commit `ecfc5a8d54462e18e13c72173e2623d16d8e25a0` (2026-05-22). Dependencies were checked on 2026-09-21. `Sources/WireGuardKitGo/go.mod` records WireGuard, `x/sys`, and six indirect dependencies: `x/crypto`, `x/exp`, `x/net`, `x/time`, Wintun, and gVisor.

For optional netstack support, gVisor uses the [Go-compatible branch](https://github.com/google/gvisor#using-go-get), pinned to `501da953ee3803d88004cba68c7cc85098c75dfb`; its Bazel `master` cannot be built with standard Go tooling.

Update these dependencies explicitly, resolve WireGuard `master` and gVisor `go` to exact pseudo-versions, and run the Go tests and both application builds. Keep the manifest and checksums minimal with `go mod tidy`. Release builds use the recorded versions without querying branches.

## Swift 6 compatibility

`TunnelConfiguration` is now a value type: edit a `var` copy and pass it back explicitly. Keys are immutable final `Sendable` types; `BaseKey` is a protocol. Adapter callbacks are `@Sendable`; app tunnel management and UI delegates are isolated to `MainActor`. Internal target names and bundle identifiers are unchanged.

## WireGuardKit integration

1. Open your Xcode project and add the Swift package with the following URL:
   
   ```
   https://github.com/skarm/wireward.git
   ```
   
2. `WireGuardKit` links against `wireguard-go-bridge` library, but it cannot build it automatically
   due to Swift package manager limitations. So it needs a little help from a developer. 
   Please follow the instructions below to create a build target(s) for `wireguard-go-bridge`.
   
   - In Xcode, click File -> New -> Target. Switch to "Other" tab and choose "External Build 
     System".
   - Type in `WireGuardGoBridge<PLATFORM>` under the "Product name", replacing the `<PLATFORM>` 
     placeholder with the name of the platform. For example, when targeting macOS use `macOS`, or 
     when targeting iOS use `iOS`.
     Make sure the build tool is set to: `/usr/bin/make` (default).
   - In the appeared "Info" tab of a newly created target, type in the "Directory" path under 
     the "External Build Tool Configuration":
     
     ```
     ${BUILD_DIR%Build/*}SourcePackages/checkouts/wireward/Sources/WireGuardKitGo
     ```
     
   - Switch to "Build Settings" and find `SDKROOT`.
     Type in `macosx` if you target macOS, or type in `iphoneos` if you target iOS.
   
3. Go to Xcode project settings and locate your network extension target and switch to 
   "Build Phases" tab.
   
   - Locate "Dependencies" section and hit "+" to add `WireGuardGoBridge<PLATFORM>` replacing 
     the `<PLATFORM>` placeholder with the name of platform matching the network extension 
     deployment target (i.e macOS or iOS).
     
   - Locate the "Link with binary libraries" section and hit "+" to add `WireGuardKit`.
   
4. In Xcode project settings, locate your main bundle app and switch to "Build Phases" tab. 
   Locate the "Link with binary libraries" section and hit "+" to add `WireGuardKit`.
   
5. iOS only: Locate Bitcode settings under your application target, Build settings -> Enable Bitcode, 
   change the corresponding value to "No".
   
Note that if you ship your app for both iOS and macOS, make sure to repeat the steps 2-4 twice, 
once per platform.

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
