// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

type platformSpec struct {
	name          string
	minimum       string
	architectures []string
}

type slice struct {
	SDK              sdk
	Architectures    []string
	DeploymentTarget string
	Key              string
	SHA256           string
}

func (b *builder) buildSlice(s *slice, overlay string) error {
	s.Key = b.sliceKey(*s)
	dir := filepath.Join(b.cache, "slices", s.Key)
	archive := filepath.Join(dir, "libwg-go.a")
	if sum, err := os.ReadFile(filepath.Join(dir, "sha256")); err == nil {
		if actual, err := fileHash(archive); err == nil && actual == string(sum) {
			s.SHA256 = actual
			fmt.Println("Cached", s.SDK.Name, strings.Join(s.Architectures, ","))
			return nil
		}
	}
	return b.compileSlice(s, overlay, dir, archive)
}

func (b *builder) compileSlice(s *slice, overlay, dir, archive string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	var archives []string
	for _, arch := range s.Architectures {
		goos, goarch, target, err := target(s.SDK.Name, arch, s.DeploymentTarget)
		if err != nil {
			return err
		}
		flags := []string{"-O2", "-g0", "-target", target, "-isysroot", s.SDK.Path}
		env := buildEnv(b.env, map[string]string{
			"GOROOT":       b.goRoot,
			"GOOS":         goos,
			"GOARCH":       goarch,
			"CGO_ENABLED":  "1",
			"CC":           joinFlags([]string{s.SDK.CC}),
			"CGO_CFLAGS":   joinFlags(flags),
			"CGO_LDFLAGS":  joinFlags(flags),
			"ZERO_AR_DATE": "1",
		})
		path := filepath.Join(dir, arch+".a")
		fmt.Println("Building", s.SDK.Name, arch, "deployment", s.DeploymentTarget)
		linkFlags := "-w -buildid="
		if s.SDK.Name == "macosx" {
			// Go's Mach-O linker otherwise uses its own minimum OS / SDK
			// defaults, independently of the deployment flags passed to Clang.
			linkFlags += " -macos=" + s.DeploymentTarget + " -macsdk=" + s.SDK.Version
		}
		_, err = b.command(env, b.goTool, "build",
			"-trimpath", "-buildvcs=false", "-overlay", overlay,
			"-ldflags="+linkFlags, "-buildmode=c-archive", "-o", path, ".",
		)
		if err != nil {
			return err
		}
		archives = append(archives, path)
	}
	args := append([]string{"lipo", "-create", "-output", archive}, archives...)
	if _, err := b.command(b.env, "/usr/bin/xcrun", args...); err != nil {
		return err
	}
	var err error
	s.SHA256, err = fileHash(archive)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "sha256"), []byte(s.SHA256), 0644)
}

func target(platform, arch, min string) (string, string, string, error) {
	goarch := map[string]string{"arm64": "arm64", "x86_64": "amd64"}[arch]
	if goarch == "" {
		return "", "", "", fmt.Errorf("unsupported architecture %q", arch)
	}
	switch platform {
	case "macosx":
		return "darwin", goarch, arch + "-apple-macos" + min, nil
	case "iphoneos":
		if arch == "arm64" {
			return "ios", goarch, arch + "-apple-ios" + min, nil
		}
	case "iphonesimulator":
		return "ios", goarch, arch + "-apple-ios" + min + "-simulator", nil
	}
	return "", "", "", fmt.Errorf("unsupported platform/architecture %s/%s", platform, arch)
}

func architectures(value string) ([]string, error) {
	archs := strings.Fields(value)
	if len(archs) == 0 {
		return nil, errors.New("at least one architecture is required")
	}
	slices.Sort(archs)
	for i, arch := range archs {
		if arch != "arm64" && arch != "x86_64" {
			return nil, fmt.Errorf("unsupported architecture %q", arch)
		}
		if i > 0 && archs[i-1] == arch {
			return nil, fmt.Errorf("duplicate architecture %q", arch)
		}
	}
	return archs, nil
}

func deploymentVersion(version string, minimum int) error {
	parts := strings.Split(version, ".")
	if len(parts) > 3 {
		return fmt.Errorf("invalid deployment target %q", version)
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || strconv.Itoa(n) != part || (i == 0 && n < minimum) {
			return fmt.Errorf("deployment target %q must be a version >= %d", version, minimum)
		}
	}
	return nil
}
