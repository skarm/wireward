// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeOverlayPreservesToolchainAndRejectsBadPatch(t *testing.T) {
	root := t.TempDir()
	b := builder{
		source: filepath.Join(root, "source"),
		cache:  filepath.Join(root, "cache"),
		goRoot: filepath.Join(root, "goroot"),
		env:    buildEnv(os.Environ(), nil),
		manifest: manifest{
			Go:            "go1.27.1",
			Inputs:        map[string]string{},
			RuntimeInputs: map[string]string{},
		},
	}
	if err := os.MkdirAll(b.source, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range runtimeFiles {
		path := filepath.Join(b.goRoot, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("original\n"), 0644); err != nil {
			t.Fatal(err)
		}
		b.manifest.RuntimeInputs[name], _ = fileHash(path)
	}
	patch := filepath.Join(b.source, "goruntime-boottime-over-monotonic.diff")
	good := "--- a/src/runtime/sys_darwin.go\n+++ b/src/runtime/sys_darwin.go\n@@ -1 +1 @@\n-original\n+patched\n"
	if err := os.WriteFile(patch, []byte(good), 0644); err != nil {
		t.Fatal(err)
	}
	b.manifest.Inputs["goruntime-boottime-over-monotonic.diff"], _ = fileHash(patch)
	overlay, err := b.prepareRuntime()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(overlay)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct{ Replace map[string]string }
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Replace) != 3 {
		t.Fatalf("unexpected overlay: %s", data)
	}
	original := filepath.Join(b.goRoot, runtimeFiles[0])
	data, err = os.ReadFile(original)
	if err != nil || string(data) != "original\n" {
		t.Fatal("installed GOROOT was changed", err)
	}
	data, err = os.ReadFile(decoded.Replace[original])
	if err != nil || string(data) != "patched\n" {
		t.Fatal("overlay was not patched", err)
	}
	if _, err := b.prepareRuntime(); err != nil {
		t.Fatalf("repeated build failed: %v", err)
	}
	if err := os.WriteFile(patch, []byte(strings.Replace(good, "-original", "-wrong", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	b.manifest.Inputs["goruntime-boottime-over-monotonic.diff"], _ = fileHash(patch)
	if _, err := b.prepareRuntime(); err == nil {
		t.Fatal("accepted a patch that does not apply")
	}
}
