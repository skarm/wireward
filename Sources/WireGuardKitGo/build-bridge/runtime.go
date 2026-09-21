// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
)

var runtimeFiles = []string{
	"src/runtime/sys_darwin.go",
	"src/runtime/sys_darwin_amd64.s",
	"src/runtime/sys_darwin_arm64.s",
}

// prepareRuntime patches an overlay without modifying the installed toolchain.
func (b *builder) prepareRuntime() (string, error) {
	key := digest(struct {
		Go, Patch string
		Files     map[string]string
	}{
		Go:    b.manifest.Go,
		Patch: b.manifest.Inputs["goruntime-boottime-over-monotonic.diff"],
		Files: b.manifest.RuntimeInputs,
	})
	dir := filepath.Join(b.cache, "runtime", key)
	// Recreate three source files; never reuse a partially applied patch.
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	replacements := make(map[string]string)
	for _, name := range runtimeFiles {
		original, patched := filepath.Join(b.goRoot, name), filepath.Join(dir, name)
		if err := copyFile(original, patched); err != nil {
			return "", err
		}
		replacements[original] = patched
	}
	_, err := b.command(b.env, "/usr/bin/patch",
		"--batch", "--fuzz=0", "-p1", "-d", dir,
		"-i", filepath.Join(b.source, "goruntime-boottime-over-monotonic.diff"),
	)
	if err != nil {
		return "", err
	}
	overlay := filepath.Join(dir, "overlay.json")
	err = writeJSON(overlay, struct{ Replace map[string]string }{replacements})
	return overlay, err
}
