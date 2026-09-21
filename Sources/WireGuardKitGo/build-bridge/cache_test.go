// SPDX-License-Identifier: MIT

package main

import "testing"

func TestCacheInvalidatesBuildInputs(t *testing.T) {
	fresh := func() (builder, slice) {
		b := builder{manifest: manifest{
			Go:        "go1.27.1",
			GoFIPS140: "off",
			Xcode:     "Xcode 27.0",
			Inputs: map[string]string{
				"go.mod":                                 "a",
				"go.sum":                                 "b",
				"api-apple.go":                           "c",
				"goruntime-boottime-over-monotonic.diff": "d",
				"Makefile":                               "e",
				"build-bridge/main.go":                   "f",
			},
			RuntimeInputs: map[string]string{"src/runtime/sys_darwin.go": "runtime"},
		}}
		s := slice{
			SDK: sdk{
				Name:           "macosx",
				Version:        "27.0",
				Build:          "27A",
				Clang:          "clang 21",
				SettingsSHA256: "sdk",
			},
			Architectures:    []string{"arm64"},
			DeploymentTarget: "13.0",
		}
		return b, s
	}
	changes := map[string]func(*builder, *slice){
		"Go":            func(b *builder, s *slice) { b.manifest.Go = "go1.27.2" },
		"FIPS mode":     func(b *builder, s *slice) { b.manifest.GoFIPS140 = "latest" },
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
