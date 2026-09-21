// SPDX-License-Identifier: MIT

package main

import (
	"slices"
	"strconv"
	"strings"
)

const goFIPS140 = "off"

func buildEnv(inherited []string, values map[string]string) []string {
	// Keep credentials, proxies and cache locations, but exclude ambient build
	// flags and Xcode's SDK/deployment settings from cross-platform compilation.
	remove := []string{
		"GOROOT", "GOOS", "GOARCH", "GOARM64", "GOAMD64",
		"GOEXPERIMENT", "GOFLAGS", "GOENV", "GOTOOLCHAIN", "GOWORK",
		"GOFIPS140", "GO_EXTLINK_ENABLED",
		"CC", "CXX", "SDKROOT", "MACOSX_DEPLOYMENT_TARGET", "IPHONEOS_DEPLOYMENT_TARGET",
		"CPATH", "C_INCLUDE_PATH", "CPLUS_INCLUDE_PATH", "LIBRARY_PATH",
	}
	env := []string{}
	for _, entry := range inherited {
		key, _, _ := strings.Cut(entry, "=")
		if !slices.Contains(remove, key) && !strings.HasPrefix(key, "CGO_") && key != "PATH" && key != "ZERO_AR_DATE" {
			env = append(env, entry)
		}
	}
	// This bridge uses the toolchain's normal cryptography and default linker
	// selection. Ambient FIPS/linker overrides must not change cached artifacts.
	env = append(env,
		"PATH=/usr/bin:/bin:/usr/sbin:/sbin",
		"GOENV=off",
		"GOTOOLCHAIN=local",
		"GOWORK=off",
		"GOFLAGS=",
		"GOFIPS140="+goFIPS140,
	)
	for key, value := range values {
		env = append(env, key+"="+value)
	}
	return env
}

func joinFlags(flags []string) string {
	quoted := make([]string, len(flags))
	for i, flag := range flags {
		quoted[i] = strconv.Quote(flag)
	}
	return strings.Join(quoted, " ")
}
