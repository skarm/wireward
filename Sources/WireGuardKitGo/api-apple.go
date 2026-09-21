/* SPDX-License-Identifier: MIT
 * Copyright (C) 2018-2019 Jason A. Donenfeld <Jason@zx2c4.com>. All Rights Reserved.
 */

package main

// #include <stdlib.h>
// #include <stdint.h>
// typedef void (*logger_fn_t)(void *, int32_t, const char *);
// static void callLogger(logger_fn_t fn, void *ctx, int32_t level, const char *msg)
// {
//     fn(ctx, level, msg);
// }
import "C"

import (
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

var loggerState struct {
	sync.RWMutex
	fn      C.logger_fn_t
	context unsafe.Pointer
}

type CLogger int32

func (l CLogger) Printf(format string, args ...any) {
	loggerState.RLock()
	defer loggerState.RUnlock()
	if loggerState.fn == nil {
		return
	}
	message := C.CString(fmt.Sprintf(format, args...))
	defer C.free(unsafe.Pointer(message))
	C.callLogger(loggerState.fn, loggerState.context, C.int32_t(l), message)
}

func init() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, unix.SIGUSR2)
	go func() {
		for range signals {
			CLogger(0).Printf("%s", captureStacks(runtime.Stack))
		}
	}()
}

//export wgSetLogger
func wgSetLogger(context unsafe.Pointer, loggerFn C.logger_fn_t) {
	// Returning guarantees no callback still uses the previous context.
	loggerState.Lock()
	defer loggerState.Unlock()
	loggerState.context, loggerState.fn = context, loggerFn
}

//export wgTurnOn
func wgTurnOn(settings *C.char, tunFd C.int32_t) C.int32_t {
	if settings == nil {
		return -C.int32_t(unix.EINVAL)
	}
	if tunFd < 0 {
		return -C.int32_t(unix.EBADF)
	}
	logger := &device.Logger{Verbosef: CLogger(0).Printf, Errorf: CLogger(1).Printf}
	dupFd, err := unix.FcntlInt(uintptr(tunFd), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return C.int32_t(errorCode(err))
	}
	if err = unix.SetNonblock(dupFd, true); err != nil {
		unix.Close(dupFd)
		return C.int32_t(errorCode(err))
	}
	// The pinned Darwin CreateTUNFromFile takes ownership on success AND failure.
	// Closing dupFd here again could close an unrelated descriptor reused by the OS.
	tunnel, err := tun.CreateTUNFromFile(os.NewFile(uintptr(dupFd), "/dev/tun"), 0)
	if err != nil {
		return C.int32_t(errorCode(err))
	}
	dev := device.NewDevice(tunnel, conn.NewStdNetBind(), logger)
	return C.int32_t(tunnels.start(dev, logger, C.GoString(settings)))
}

//export wgTurnOff
func wgTurnOff(handle C.int32_t) C.int64_t { return C.int64_t(tunnels.stop(int32(handle))) }

//export wgSetConfig
func wgSetConfig(handle C.int32_t, settings *C.char) C.int64_t {
	if settings == nil {
		return -C.int64_t(unix.EINVAL)
	}
	return C.int64_t(tunnels.withDevice(int32(handle), func(dev backendDevice) error { return dev.IpcSet(C.GoString(settings)) }))
}

//export wgGetConfig
func wgGetConfig(handle C.int32_t, settings **C.char) C.int64_t {
	if settings == nil {
		return -C.int64_t(unix.EINVAL)
	}
	*settings = nil
	var value string
	code := tunnels.withDevice(int32(handle), func(dev backendDevice) (err error) { value, err = dev.IpcGet(); return })
	if code == 0 {
		*settings = C.CString(value)
	}
	return C.int64_t(code)
}

//export wgBumpSockets
func wgBumpSockets(handle C.int32_t) C.int64_t { return C.int64_t(tunnels.bump(int32(handle))) }

//export wgDisableSomeRoamingForBrokenMobileSemantics
func wgDisableSomeRoamingForBrokenMobileSemantics(handle C.int32_t) C.int64_t {
	return C.int64_t(tunnels.withDevice(int32(handle), func(dev backendDevice) error {
		dev.DisableSomeRoamingForBrokenMobileSemantics()
		return nil
	}))
}

//export wgFreeString
func wgFreeString(value *C.char) { C.free(unsafe.Pointer(value)) }

//export wgVersion
func wgVersion() *C.char {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return C.CString("unknown")
	}
	for _, dep := range info.Deps {
		if dep.Path == "golang.zx2c4.com/wireguard" {
			parts := strings.Split(dep.Version, "-")
			if len(parts) == 3 && len(parts[2]) == 12 {
				return C.CString(parts[2][:7])
			}
			return C.CString(dep.Version)
		}
	}
	return C.CString("unknown")
}

func main() {}
