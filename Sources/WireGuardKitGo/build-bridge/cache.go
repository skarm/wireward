// SPDX-License-Identifier: MIT

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// manifest records the inputs and checksums published beside the XCFramework.
type manifest struct {
	Go            string
	GoFIPS140     string
	Xcode         string
	Inputs        map[string]string
	RuntimeInputs map[string]string
	Backend       module
	Slices        []slice
}

// inspectInputs records every source, build script and runtime patch input.
func (b *builder) inspectInputs() error {
	b.manifest.Inputs = make(map[string]string)
	err := filepath.WalkDir(b.source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(b.source, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if rel != "." && rel != "build-bridge" {
				return filepath.SkipDir
			}
			return nil
		}
		isSource := slices.Contains([]string{".go", ".c", ".h", ".s", ".S"}, filepath.Ext(rel))
		isBuildInput := slices.Contains([]string{
			"go.mod", "go.sum", "Makefile", "module.modulemap",
			"goruntime-boottime-over-monotonic.diff",
		}, rel)
		if isSource || isBuildInput {
			sum, err := fileHash(path)
			if err != nil {
				return err
			}
			b.manifest.Inputs[rel] = sum
		}
		return nil
	})
	if err != nil {
		return err
	}
	b.manifest.RuntimeInputs = make(map[string]string)
	for _, name := range runtimeFiles {
		sum, err := fileHash(filepath.Join(b.goRoot, name))
		if err != nil {
			return err
		}
		b.manifest.RuntimeInputs[name] = sum
	}
	return nil
}

func (b *builder) sliceKey(s slice) string {
	s.Key, s.SHA256 = "", ""
	return digest(struct {
		Go, GoFIPS140, Xcode string
		Inputs, Runtime      map[string]string
		Slice                slice
	}{
		Go:        b.manifest.Go,
		GoFIPS140: b.manifest.GoFIPS140,
		Xcode:     b.manifest.Xcode,
		Inputs:    b.manifest.Inputs,
		Runtime:   b.manifest.RuntimeInputs,
		Slice:     s,
	})
}

func digest(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		// Cache inputs contain only structs/maps of strings.
		panic(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func fileHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

func copyFile(from, to string) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0755); err != nil {
		return err
	}
	return os.WriteFile(to, data, 0644)
}
