// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestXCFrameworkMetadataIsDeterministic(t *testing.T) {
	root := t.TempDir()
	b := builder{source: root, env: buildEnv(os.Environ(), nil)}
	inputs := []string{
		`{
  "AvailableLibraries": [
    {
      "LibraryIdentifier": "macos-arm64_x86_64",
      "SupportedArchitectures": [
        "x86_64",
        "arm64"
      ],
      "LibraryPath": "libwg-go.a"
    },
    {
      "LibraryIdentifier": "ios-arm64",
      "SupportedArchitectures": [
        "arm64"
      ],
      "LibraryPath": "libwg-go.a"
    }
  ],
  "XCFrameworkFormatVersion": "1.0",
  "CFBundlePackageType": "XFWK"
}`,
		`{
  "CFBundlePackageType": "XFWK",
  "XCFrameworkFormatVersion": "1.0",
  "AvailableLibraries": [
    {
      "LibraryPath": "libwg-go.a",
      "SupportedArchitectures": [
        "arm64"
      ],
      "LibraryIdentifier": "ios-arm64"
    },
    {
      "SupportedArchitectures": [
        "arm64",
        "x86_64"
      ],
      "LibraryPath": "libwg-go.a",
      "LibraryIdentifier": "macos-arm64_x86_64"
    }
  ]
}`,
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
