// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEnvironmentDoesNotLeakBuildSettings(t *testing.T) {
	inherited := []string{
		"PATH=/untrusted",
		"GOFLAGS=-race",
		"GOTOOLCHAIN=auto",
		"GOWORK=/other/go.work",
		"GOROOT=/other",
		"GOOS=linux",
		"GOARCH=386",
		"GOEXPERIMENT=fieldtrack",
		"CGO_CFLAGS=-march=native",
		"CGO_CFLAGS_ALLOW=.*",
		"CGO_LDFLAGS=-L/other",
		"SDKROOT=/wrong/sdk",
		"IPHONEOS_DEPLOYMENT_TARGET=99",
		"CPATH=/other",
		"CC=/other/cc",
		"GOCACHE=/cache",
		"HTTPS_PROXY=http://proxy",
		"GOFIPS140=latest",
		"GO_EXTLINK_ENABLED=0",
	}
	env := buildEnv(inherited, map[string]string{
		"GOOS":       "ios",
		"GOARCH":     "arm64",
		"CC":         "/xcode/clang",
		"CGO_CFLAGS": "-O2",
	})
	got := map[string]string{}
	for _, item := range env {
		k, v, _ := strings.Cut(item, "=")
		if _, exists := got[k]; exists {
			t.Fatalf("duplicate %s", k)
		}
		got[k] = v
	}
	for k, v := range map[string]string{
		"GOOS":        "ios",
		"GOARCH":      "arm64",
		"CC":          "/xcode/clang",
		"CGO_CFLAGS":  "-O2",
		"GOTOOLCHAIN": "local",
		"GOENV":       "off",
		"GOWORK":      "off",
		"GOFLAGS":     "",
		"PATH":        "/usr/bin:/bin:/usr/sbin:/sbin",
		"GOCACHE":     "/cache",
		"HTTPS_PROXY": "http://proxy",
	} {
		if got[k] != v {
			t.Errorf("%s = %q; want %q", k, got[k], v)
		}
	}
	for _, k := range []string{
		"GOROOT", "GOEXPERIMENT", "CGO_CFLAGS_ALLOW", "CGO_LDFLAGS",
		"SDKROOT", "IPHONEOS_DEPLOYMENT_TARGET", "CPATH",
	} {
		if _, exists := got[k]; exists {
			t.Errorf("inherited %s", k)
		}
	}
	if got["GOFIPS140"] != "off" {
		t.Errorf("uncontrolled cryptography mode: %q", got["GOFIPS140"])
	}
	if _, exists := got["GO_EXTLINK_ENABLED"]; exists {
		t.Fatal("inherited linker mode")
	}
}

func TestCryptoModeDoesNotDependOnAmbientFIPSSetting(t *testing.T) {
	file := filepath.Join(t.TempDir(), "main.go")
	program := `package main
import (
    "crypto/fips140"
    "fmt"
)
func main() { fmt.Println(fips140.Enabled()) }
`
	if err := os.WriteFile(file, []byte(program), 0600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"off", "latest", "invalid-ambient-value"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("GOFIPS140", mode)
			cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin/go"), "run", file)
			cmd.Env = buildEnv(os.Environ(), map[string]string{"CGO_ENABLED": "0"})
			out, err := cmd.CombinedOutput()
			if err != nil || strings.TrimSpace(string(out)) != "false" {
				t.Fatalf("unexpected crypto mode with inherited GOFIPS140=%s: %s (%v)", mode, out, err)
			}
		})
	}
}
