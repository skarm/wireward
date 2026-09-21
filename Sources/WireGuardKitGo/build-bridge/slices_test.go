// SPDX-License-Identifier: MIT

package main

import "testing"

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
