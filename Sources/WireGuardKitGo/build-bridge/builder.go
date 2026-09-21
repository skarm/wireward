// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

type builder struct {
	source   string
	cache    string
	output   string
	goTool   string
	goRoot   string
	env      []string
	manifest manifest
}

// build serializes cache and artifact writes, then prepares all platform slices.
func (b *builder) build(platforms []platformSpec) error {
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
	for _, spec := range platforms {
		sdk, err := b.inspectSDK(spec.name)
		if err != nil {
			return err
		}
		b.manifest.Slices = append(b.manifest.Slices, slice{
			SDK:              sdk,
			Architectures:    spec.architectures,
			DeploymentTarget: spec.minimum,
		})
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
