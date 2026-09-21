// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const modulePath = "golang.zx2c4.com/wireguard"

type module struct {
	Path     string
	Version  string
	Sum      string
	GoModSum string
	Replace  *module `json:",omitempty"`
}

type sdk struct {
	Name           string
	Version        string
	Build          string
	Clang          string
	SettingsSHA256 string
	Path           string
	CC             string
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
	b.manifest.GoFIPS140 = goFIPS140
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
	return b.inspectInputs()
}

func (b *builder) inspectSDK(name string) (sdk, error) {
	s := sdk{Name: name}
	for option, dest := range map[string]*string{
		"--show-sdk-path":          &s.Path,
		"--show-sdk-version":       &s.Version,
		"--show-sdk-build-version": &s.Build,
	} {
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
