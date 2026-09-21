// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
)

func (b *builder) packageFramework() error {
	if err := os.MkdirAll(b.output, 0755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(b.output, ".bridge-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	headers := filepath.Join(stage, "Headers")
	for _, name := range []string{"wireguard.h", "module.modulemap"} {
		if err := copyFile(filepath.Join(b.source, name), filepath.Join(headers, name)); err != nil {
			return err
		}
	}
	header := "// Generated from go list -m -json; do not edit.\n#define WIREGUARD_GO_VERSION " + strconv.Quote(b.manifest.Backend.Version) + "\n"
	if err := os.WriteFile(filepath.Join(headers, "wireguard-go-version.h"), []byte(header), 0644); err != nil {
		return err
	}
	framework := filepath.Join(stage, "WireGuardKitGo.xcframework")
	args := []string{"-create-xcframework"}
	for _, s := range b.manifest.Slices {
		args = append(args, "-library", filepath.Join(b.cache, "slices", s.Key, "libwg-go.a"), "-headers", headers)
	}
	args = append(args, "-output", framework)
	if _, err := b.command(b.env, "/usr/bin/xcodebuild", args...); err != nil {
		return err
	}
	if err := b.normalizeXCFramework(filepath.Join(framework, "Info.plist")); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(stage, "WireGuardKitGo.build.json"), b.manifest); err != nil {
		return err
	}
	// Publish only complete builds; compilation cannot destroy the previous one.
	dest := filepath.Join(b.output, "WireGuardKitGo.xcframework")
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	if err := os.Rename(framework, dest); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(stage, "WireGuardKitGo.build.json"), filepath.Join(b.output, "WireGuardKitGo.build.json")); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(headers, "wireguard-go-version.h"), filepath.Join(b.output, "wireguard-go-version.h")); err != nil {
		return err
	}
	fmt.Println("Created", dest)
	return nil
}

// xcodebuild emits AvailableLibraries in an unspecified order. Normalize both
// arrays before serializing the plist so packaging is reproducible too.
func (b *builder) normalizeXCFramework(path string) error {
	data, err := b.command(b.env, "/usr/bin/plutil", "-convert", "json", "-o", "-", path)
	if err != nil {
		return err
	}
	var info map[string]json.RawMessage
	if err := json.Unmarshal([]byte(data), &info); err != nil {
		return err
	}
	var libraries []map[string]json.RawMessage
	if err := json.Unmarshal(info["AvailableLibraries"], &libraries); err != nil {
		return err
	}
	byID := make(map[string]map[string]json.RawMessage)
	var ids []string
	for _, library := range libraries {
		var id string
		if err := json.Unmarshal(library["LibraryIdentifier"], &id); err != nil {
			return err
		}
		if id == "" || byID[id] != nil {
			return fmt.Errorf("invalid or duplicate XCFramework slice %q", id)
		}
		var archs []string
		if err := json.Unmarshal(library["SupportedArchitectures"], &archs); err != nil {
			return err
		}
		slices.Sort(archs)
		library["SupportedArchitectures"], err = json.Marshal(archs)
		if err != nil {
			return err
		}
		ids = append(ids, id)
		byID[id] = library
	}
	slices.Sort(ids)
	for i, id := range ids {
		libraries[i] = byID[id]
	}
	info["AvailableLibraries"], err = json.Marshal(libraries)
	if err != nil {
		return err
	}
	jsonPath := path + ".json"
	defer os.Remove(jsonPath)
	if err := writeJSON(jsonPath, info); err != nil {
		return err
	}
	_, err = b.command(b.env, "/usr/bin/plutil", "-convert", "xml1", "-o", path, jsonPath)
	return err
}
