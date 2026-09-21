// SPDX-License-Identifier: MIT
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppleTargets(t *testing.T) {
	for _, tc := range []struct{ platform, arch, min, os, goarch, triple string }{
		{"macosx", "arm64", "13.0", "darwin", "arm64", "arm64-apple-macos13.0"},
		{"macosx", "x86_64", "14.2", "darwin", "amd64", "x86_64-apple-macos14.2"},
		{"iphoneos", "arm64", "15.0", "ios", "arm64", "arm64-apple-ios15.0"},
		{"iphonesimulator", "arm64", "15.0", "ios", "arm64", "arm64-apple-ios15.0-simulator"},
		{"iphonesimulator", "x86_64", "15.0", "ios", "amd64", "x86_64-apple-ios15.0-simulator"},
	} {
		t.Run(tc.triple, func(t *testing.T) {
			os, arch, triple, err := target(tc.platform, tc.arch, tc.min)
			if err != nil || os != tc.os || arch != tc.goarch || triple != tc.triple {
				t.Fatalf("got %s %s %s %v", os, arch, triple, err)
			}
		})
	}
	for _, tc := range [][2]string{{"iphoneos", "x86_64"}, {"macosx", "i386"}, {"unknown", "arm64"}} {
		if _, _, _, err := target(tc[0], tc[1], "15.0"); err == nil {
			t.Fatalf("accepted unsupported target %v", tc)
		}
	}
}

func TestInvalidBuildOptions(t *testing.T) {
	for _, value := range []string{"", "armv7", "arm64 arm64", "x86_64 i386"} {
		if _, err := architectures(value); err == nil {
			t.Fatalf("accepted architecture list %q", value)
		}
	}
	for _, value := range []string{"", "12.9", "13.0-simulator", "13..0", "13.-1", "+13", "13.00", "13.0.0.1"} {
		if err := deploymentVersion(value, 13); err == nil {
			t.Fatalf("accepted deployment target %q", value)
		}
	}
	for _, value := range []string{"13", "13.0", "15.2.1"} {
		if err := deploymentVersion(value, 13); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEnvironmentDoesNotLeakBuildSettings(t *testing.T) {
	inherited := []string{"PATH=/untrusted", "GOFLAGS=-race", "GOTOOLCHAIN=auto", "GOWORK=/other/go.work", "GOROOT=/other", "GOOS=linux", "GOARCH=386", "GOEXPERIMENT=fieldtrack", "CGO_CFLAGS=-march=native", "CGO_CFLAGS_ALLOW=.*", "CGO_LDFLAGS=-L/other", "SDKROOT=/wrong/sdk", "IPHONEOS_DEPLOYMENT_TARGET=99", "CPATH=/other", "CC=/other/cc", "GOCACHE=/cache", "HTTPS_PROXY=http://proxy"}
	env := buildEnv(inherited, map[string]string{"GOOS": "ios", "GOARCH": "arm64", "CC": "/xcode/clang", "CGO_CFLAGS": "-O2"})
	got := map[string]string{}
	for _, item := range env {
		k, v, _ := strings.Cut(item, "=")
		if _, exists := got[k]; exists {
			t.Fatalf("duplicate %s", k)
		}
		got[k] = v
	}
	for k, v := range map[string]string{"GOOS": "ios", "GOARCH": "arm64", "CC": "/xcode/clang", "CGO_CFLAGS": "-O2", "GOTOOLCHAIN": "local", "GOENV": "off", "GOWORK": "off", "GOFLAGS": "", "PATH": "/usr/bin:/bin:/usr/sbin:/sbin", "GOCACHE": "/cache", "HTTPS_PROXY": "http://proxy"} {
		if got[k] != v {
			t.Errorf("%s = %q; want %q", k, got[k], v)
		}
	}
	for _, k := range []string{"GOROOT", "GOEXPERIMENT", "CGO_CFLAGS_ALLOW", "CGO_LDFLAGS", "SDKROOT", "IPHONEOS_DEPLOYMENT_TARGET", "CPATH"} {
		if _, exists := got[k]; exists {
			t.Errorf("inherited %s", k)
		}
	}
}

func TestCacheInvalidatesBuildInputs(t *testing.T) {
	fresh := func() (builder, slice) {
		return builder{manifest: manifest{Go: "go1.27.1", Xcode: "Xcode 27.0", Inputs: map[string]string{"go.mod": "a", "go.sum": "b", "api-apple.go": "c", "goruntime-boottime-over-monotonic.diff": "d", "Makefile": "e", "build-bridge/main.go": "f"}, RuntimeInputs: map[string]string{"src/runtime/sys_darwin.go": "runtime"}}}, slice{SDK: sdk{Name: "macosx", Version: "27.0", Build: "27A", Clang: "clang 21", SettingsSHA256: "sdk"}, Architectures: []string{"arm64"}, DeploymentTarget: "13.0"}
	}
	changes := map[string]func(*builder, *slice){
		"Go":            func(b *builder, s *slice) { b.manifest.Go = "go1.27.2" },
		"Xcode":         func(b *builder, s *slice) { b.manifest.Xcode = "Xcode 27.1" },
		"runtime":       func(b *builder, s *slice) { b.manifest.RuntimeInputs["src/runtime/sys_darwin.go"] = "changed" },
		"platform":      func(b *builder, s *slice) { s.SDK.Name = "iphonesimulator" },
		"SDK version":   func(b *builder, s *slice) { s.SDK.Version = "27.1" },
		"SDK build":     func(b *builder, s *slice) { s.SDK.Build = "27B" },
		"SDK settings":  func(b *builder, s *slice) { s.SDK.SettingsSHA256 = "changed" },
		"SDK path":      func(b *builder, s *slice) { s.SDK.Path = "/another/SDK" },
		"compiler path": func(b *builder, s *slice) { s.SDK.CC = "/another/clang" },
		"clang":         func(b *builder, s *slice) { s.SDK.Clang = "clang 22" },
		"arch":          func(b *builder, s *slice) { s.Architectures = []string{"arm64", "x86_64"} },
		"deployment":    func(b *builder, s *slice) { s.DeploymentTarget = "14.0" },
	}
	base, spec := fresh()
	for name := range base.manifest.Inputs {
		changes[name] = func(b *builder, s *slice) { b.manifest.Inputs[name] = "changed" }
	}
	key := base.sliceKey(spec)
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			b, s := fresh()
			change(&b, &s)
			if b.sliceKey(s) == key {
				t.Fatal("reused stale cache key")
			}
		})
	}
	spec.Key = "old"
	spec.SHA256 = "old"
	if base.sliceKey(spec) != key {
		t.Fatal("output digest affected cache key")
	}
}

func TestRuntimeOverlayPreservesToolchainAndRejectsBadPatch(t *testing.T) {
	root := t.TempDir()
	b := builder{source: filepath.Join(root, "source"), cache: filepath.Join(root, "cache"), goRoot: filepath.Join(root, "goroot"), env: buildEnv(os.Environ(), nil), manifest: manifest{Go: "go1.27.1", Inputs: map[string]string{}, RuntimeInputs: map[string]string{}}}
	if err := os.MkdirAll(b.source, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range runtimeFiles {
		path := filepath.Join(b.goRoot, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("original\n"), 0644); err != nil {
			t.Fatal(err)
		}
		b.manifest.RuntimeInputs[name], _ = fileHash(path)
	}
	patch := filepath.Join(b.source, "goruntime-boottime-over-monotonic.diff")
	good := "--- a/src/runtime/sys_darwin.go\n+++ b/src/runtime/sys_darwin.go\n@@ -1 +1 @@\n-original\n+patched\n"
	if err := os.WriteFile(patch, []byte(good), 0644); err != nil {
		t.Fatal(err)
	}
	b.manifest.Inputs["goruntime-boottime-over-monotonic.diff"], _ = fileHash(patch)
	overlay, err := b.prepareRuntime()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(overlay)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct{ Replace map[string]string }
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Replace) != 3 {
		t.Fatalf("unexpected overlay: %s", data)
	}
	original := filepath.Join(b.goRoot, runtimeFiles[0])
	data, err = os.ReadFile(original)
	if err != nil || string(data) != "original\n" {
		t.Fatal("installed GOROOT was changed", err)
	}
	data, err = os.ReadFile(decoded.Replace[original])
	if err != nil || string(data) != "patched\n" {
		t.Fatal("overlay was not patched", err)
	}
	if _, err := b.prepareRuntime(); err != nil {
		t.Fatalf("repeated build failed: %v", err)
	}
	if err := os.WriteFile(patch, []byte(strings.Replace(good, "-original", "-wrong", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	b.manifest.Inputs["goruntime-boottime-over-monotonic.diff"], _ = fileHash(patch)
	if _, err := b.prepareRuntime(); err == nil {
		t.Fatal("accepted a patch that does not apply")
	}
}

func TestXCFrameworkMetadataIsDeterministic(t *testing.T) {
	root := t.TempDir()
	b := builder{source: root, env: buildEnv(os.Environ(), nil)}
	inputs := []string{
		`{"AvailableLibraries":[{"LibraryIdentifier":"macos-arm64_x86_64","SupportedArchitectures":["x86_64","arm64"],"LibraryPath":"libwg-go.a"},{"LibraryIdentifier":"ios-arm64","SupportedArchitectures":["arm64"],"LibraryPath":"libwg-go.a"}],"XCFrameworkFormatVersion":"1.0","CFBundlePackageType":"XFWK"}`,
		`{"CFBundlePackageType":"XFWK","XCFrameworkFormatVersion":"1.0","AvailableLibraries":[{"LibraryPath":"libwg-go.a","SupportedArchitectures":["arm64"],"LibraryIdentifier":"ios-arm64"},{"SupportedArchitectures":["arm64","x86_64"],"LibraryPath":"libwg-go.a","LibraryIdentifier":"macos-arm64_x86_64"}]}`,
	}
	var expected string
	for _, input := range inputs {
		path := filepath.Join(root, "Info.plist")
		if err := os.WriteFile(path, []byte(input), 0644); err != nil {
			t.Fatal(err)
		}
		if err := b.normalizeXCFramework(path); err != nil {
			t.Fatal(err)
		}
		normalized, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if expected != "" && string(normalized) != expected {
			t.Fatal("different plist bytes for equivalent XCFramework slices")
		}
		expected = string(normalized)
		if err := b.normalizeXCFramework(path); err != nil {
			t.Fatal(err)
		}
		repeated, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(repeated) != expected {
			t.Fatal("normalization is not idempotent")
		}
	}
}
