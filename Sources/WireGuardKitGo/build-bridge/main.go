// SPDX-License-Identifier: MIT

// build-bridge builds the Apple c-archive slices and packages WireGuardKitGo.
// It uses only the Go standard library; it is not part of the shipped bridge.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
)

const modulePath = "golang.zx2c4.com/wireguard"

var runtimeFiles = []string{"src/runtime/sys_darwin.go", "src/runtime/sys_darwin_amd64.s", "src/runtime/sys_darwin_arm64.s"}

type module struct {
	Path, Version, Sum, GoModSum string
	Replace                      *module `json:",omitempty"`
}
type sdk struct {
	Name, Version, Build, Clang, SettingsSHA256 string
	Path, CC                                    string
}
type slice struct {
	SDK              sdk
	Architectures    []string
	DeploymentTarget string
	Key, SHA256      string
}
type manifest struct {
	Go, Xcode     string
	Inputs        map[string]string
	RuntimeInputs map[string]string
	Backend       module
	Slices        []slice
}
type builder struct {
	source, cache, output, goTool, goRoot string
	env                                   []string
	manifest                              manifest
}

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
	b := builder{source: source, cache: cachePath, output: outputPath, goTool: *goTool}
	b.env = buildEnv(os.Environ(), nil)
	if err := os.MkdirAll(b.output, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(b.cache, 0755); err != nil {
		return err
	}
	// The kernel releases the writer lock after failures or process termination.
	lock, err := os.OpenFile(filepath.Join(b.cache, "build.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	outputLock, err := os.OpenFile(filepath.Join(b.output, ".build.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer outputLock.Close()
	if err := syscall.Flock(int(outputLock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(outputLock.Fd()), syscall.LOCK_UN)
	if err := b.inspect(); err != nil {
		return err
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
	for _, spec := range []struct {
		name, min string
		archs     []string
	}{
		{"macosx", *macMin, mac}, {"iphoneos", *iosMin, []string{"arm64"}}, {"iphonesimulator", *iosMin, sim},
	} {
		sdk, err := b.inspectSDK(spec.name)
		if err != nil {
			return err
		}
		b.manifest.Slices = append(b.manifest.Slices, slice{SDK: sdk, Architectures: spec.archs, DeploymentTarget: spec.min})
	}
	overlay, err := b.prepareRuntime()
	if err != nil {
		return err
	}
	for i := range b.manifest.Slices {
		if err := b.buildSlice(&b.manifest.Slices[i], overlay); err != nil {
			return err
		}
	}
	return b.packageFramework()
}
func (b *builder) command(env []string, program string, args ...string) (string, error) {
	cmd := exec.Command(program, args...)
	cmd.Dir, cmd.Env = b.source, env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w\n%s", program, strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out)), nil
}
func (b *builder) inspect() error {
	version, err := b.command(b.env, b.goTool, "env", "GOVERSION")
	if err != nil {
		return err
	}
	pinned, err := os.ReadFile(filepath.Join(b.source, "../../.go-version"))
	if err != nil {
		return err
	}
	if version != "go"+strings.TrimSpace(string(pinned)) || runtime.Version() != version {
		return fmt.Errorf("expected Go %s for both the driver and bridge; got %s / %s", strings.TrimSpace(string(pinned)), runtime.Version(), version)
	}
	b.manifest.Go = version
	b.goRoot, err = b.command(b.env, b.goTool, "env", "GOROOT")
	if err != nil {
		return err
	}
	b.manifest.Xcode, err = b.command(b.env, "/usr/bin/xcodebuild", "-version")
	if err != nil {
		return err
	}
	pinned, err = os.ReadFile(filepath.Join(b.source, "../../.xcode-version"))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(b.manifest.Xcode, "Xcode "+strings.TrimSpace(string(pinned))+"\n") {
		return fmt.Errorf("expected Xcode %s; got %s", strings.TrimSpace(string(pinned)), b.manifest.Xcode)
	}
	metadata, err := b.command(b.env, b.goTool, "list", "-m", "-json", modulePath)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(metadata), &b.manifest.Backend); err != nil {
		return err
	}
	if b.manifest.Backend.Version == "" || b.manifest.Backend.Replace != nil {
		return errors.New("the WireGuard module must have a pinned version and no replacement")
	}
	b.manifest.Inputs = make(map[string]string)
	err = filepath.WalkDir(b.source, func(path string, entry fs.DirEntry, err error) error {
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
		if slices.Contains([]string{".go", ".c", ".h", ".s", ".S"}, filepath.Ext(rel)) || slices.Contains([]string{"go.mod", "go.sum", "Makefile", "module.modulemap", "goruntime-boottime-over-monotonic.diff"}, rel) {
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
func (b *builder) inspectSDK(name string) (sdk, error) {
	s := sdk{Name: name}
	for option, dest := range map[string]*string{"--show-sdk-path": &s.Path, "--show-sdk-version": &s.Version, "--show-sdk-build-version": &s.Build} {
		value, err := b.command(b.env, "/usr/bin/xcrun", "--sdk", name, option)
		if err != nil {
			return s, err
		}
		*dest = value
	}
	var err error
	s.CC, err = b.command(b.env, "/usr/bin/xcrun", "--sdk", name, "--find", "clang")
	if err != nil {
		return s, err
	}
	s.Clang, err = b.command(b.env, s.CC, "--version")
	if err != nil {
		return s, err
	}
	s.SettingsSHA256, err = fileHash(filepath.Join(s.Path, "SDKSettings.json"))
	return s, err
}
func (b *builder) prepareRuntime() (string, error) {
	key := digest(struct {
		Go, Patch string
		Files     map[string]string
	}{b.manifest.Go, b.manifest.Inputs["goruntime-boottime-over-monotonic.diff"], b.manifest.RuntimeInputs})
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
	_, err := b.command(b.env, "/usr/bin/patch", "--batch", "--fuzz=0", "-p1", "-d", dir, "-i", filepath.Join(b.source, "goruntime-boottime-over-monotonic.diff"))
	if err != nil {
		return "", err
	}
	overlay := filepath.Join(dir, "overlay.json")
	err = writeJSON(overlay, struct{ Replace map[string]string }{replacements})
	return overlay, err
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

func (b *builder) sliceKey(s slice) string {
	s.Key, s.SHA256 = "", ""
	return digest(struct {
		Go, Xcode       string
		Inputs, Runtime map[string]string
		Slice           slice
	}{b.manifest.Go, b.manifest.Xcode, b.manifest.Inputs, b.manifest.RuntimeInputs, s})
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
			"GOROOT": b.goRoot, "GOOS": goos, "GOARCH": goarch, "CGO_ENABLED": "1",
			"CC": joinFlags([]string{s.SDK.CC}), "CGO_CFLAGS": joinFlags(flags), "CGO_LDFLAGS": joinFlags(flags), "ZERO_AR_DATE": "1",
		})
		path := filepath.Join(dir, arch+".a")
		fmt.Println("Building", s.SDK.Name, arch, "deployment", s.DeploymentTarget)
		linkFlags := "-w -buildid="
		if s.SDK.Name == "macosx" {
			// Go's Mach-O linker otherwise uses its own minimum OS / SDK
			// defaults, independently of the deployment flags passed to Clang.
			linkFlags += " -macos=" + s.DeploymentTarget + " -macsdk=" + s.SDK.Version
		}
		if _, err := b.command(env, b.goTool, "build", "-trimpath", "-buildvcs=false", "-overlay", overlay, "-ldflags="+linkFlags, "-buildmode=c-archive", "-o", path, "."); err != nil {
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
func buildEnv(inherited []string, values map[string]string) []string {
	// Keep credentials, proxies and cache locations, but exclude ambient build
	// flags and Xcode's SDK/deployment settings from cross-platform compilation.
	remove := []string{"GOROOT", "GOOS", "GOARCH", "GOARM64", "GOAMD64", "GOEXPERIMENT", "GOFLAGS", "GOENV", "GOTOOLCHAIN", "GOWORK", "CC", "CXX", "SDKROOT", "MACOSX_DEPLOYMENT_TARGET", "IPHONEOS_DEPLOYMENT_TARGET", "CPATH", "C_INCLUDE_PATH", "CPLUS_INCLUDE_PATH", "LIBRARY_PATH"}
	env := []string{}
	for _, entry := range inherited {
		key, _, _ := strings.Cut(entry, "=")
		if !slices.Contains(remove, key) && !strings.HasPrefix(key, "CGO_") && key != "PATH" && key != "ZERO_AR_DATE" {
			env = append(env, entry)
		}
	}
	env = append(env, "PATH=/usr/bin:/bin:/usr/sbin:/sbin", "GOENV=off", "GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS=")
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
func digest(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	} // Cache inputs contain only structs/maps of strings.
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
