// SPDX-License-Identifier: MIT

// build-bridge builds the Apple c-archive slices and packages WireGuardKitGo.
// It uses only the Go standard library; it is not part of the shipped bridge.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Go bridge:", err)
		os.Exit(1)
	}
}

func run() error {
	goTool := flag.String("go", "", "absolute path to the pinned Go toolchain")
	cache := flag.String("cache", "../../build/GoBridge", "build cache directory")
	output := flag.String("output", "../../Artifacts", "artifact directory")
	macArchs := flag.String("macos-archs", "arm64 x86_64", "macOS architectures")
	simArchs := flag.String("simulator-archs", "arm64 x86_64", "iOS simulator architectures")
	macMin := flag.String("macos-min", "13.0", "minimum macOS version")
	iosMin := flag.String("ios-min", "15.0", "minimum iOS version")
	flag.Parse()
	if !filepath.IsAbs(*goTool) {
		return errors.New("-go must be an absolute path")
	}
	if runtime.GOOS != "darwin" {
		return errors.New("an Apple host with Xcode is required")
	}
	source, err := os.Getwd()
	if err != nil {
		return err
	}
	cachePath, err := filepath.Abs(*cache)
	if err != nil {
		return err
	}
	outputPath, err := filepath.Abs(*output)
	if err != nil {
		return err
	}
	b := builder{
		source: source,
		cache:  cachePath,
		output: outputPath,
		goTool: *goTool,
		env:    buildEnv(os.Environ(), nil),
	}
	mac, err := architectures(*macArchs)
	if err != nil {
		return err
	}
	sim, err := architectures(*simArchs)
	if err != nil {
		return err
	}
	if err := deploymentVersion(*macMin, 13); err != nil {
		return err
	}
	if err := deploymentVersion(*iosMin, 15); err != nil {
		return err
	}
	return b.build([]platformSpec{
		{name: "macosx", minimum: *macMin, architectures: mac},
		{name: "iphoneos", minimum: *iosMin, architectures: []string{"arm64"}},
		{name: "iphonesimulator", minimum: *iosMin, architectures: sim},
	})
}
