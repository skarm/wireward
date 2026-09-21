// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"math"
	"sync"
	"time"

	"golang.org/x/sys/unix"
	"golang.zx2c4.com/wireguard/device"
)

// Keep lifecycle ownership testable without a privileged Darwin utun.
type backendDevice interface {
	IpcSet(string) error
	IpcGet() (string, error)
	Up() error
	Close()
	BindUpdate() error
	SendKeepalivesToPeersWithCurrentKeypair()
	DisableSomeRoamingForBrokenMobileSemantics()
}

type tunnelHandle struct {
	mu      sync.Mutex
	dev     backendDevice
	logger  *device.Logger
	closed  bool
	bumping bool
	done    chan struct{}
	workers sync.WaitGroup
}

type handleRegistry struct {
	mu      sync.Mutex
	handles map[int32]*tunnelHandle
	// Never recycle an identifier: a stale caller must not affect a new tunnel.
	next int64
}

var tunnels = handleRegistry{handles: make(map[int32]*tunnelHandle)}

func errorCode(err error) int64 {
	if err == nil {
		return 0
	}
	var ipcErr *device.IPCError
	if errors.As(err, &ipcErr) {
		return ipcErr.ErrorCode()
	}
	var errno unix.Errno
	if errors.As(err, &errno) {
		return -int64(errno)
	}
	return -int64(unix.EIO)
}

// Ownership transfers here; every unsuccessful start closes the whole device.
func (r *handleRegistry) start(dev backendDevice, logger *device.Logger, settings string) int32 {
	published := false
	defer func() {
		if !published {
			dev.Close()
		}
	}()
	if err := dev.IpcSet(settings); err != nil {
		logger.Errorf("Unable to set IPC settings: %v", err)
		return int32(errorCode(err))
	}
	if err := dev.Up(); err != nil {
		logger.Errorf("Unable to start device: %v", err)
		return int32(errorCode(err))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.next > math.MaxInt32 {
		return -int32(unix.EMFILE)
	}
	id := int32(r.next)
	r.next++
	r.handles[id] = &tunnelHandle{dev: dev, logger: logger, done: make(chan struct{})}
	published = true
	return id
}

func (r *handleRegistry) lookup(id int32) *tunnelHandle {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.handles[id]
}

func (r *handleRegistry) withDevice(id int32, operation func(backendDevice) error) int64 {
	h := r.lookup(id)
	if h == nil {
		return -int64(unix.EBADF)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return -int64(unix.EBADF)
	}
	return errorCode(operation(h.dev))
}

func (r *handleRegistry) stop(id int32) int64 {
	r.mu.Lock()
	h := r.handles[id]
	delete(r.handles, id)
	r.mu.Unlock()
	if h == nil {
		return -int64(unix.EBADF)
	}
	h.mu.Lock()
	h.closed = true
	close(h.done)
	h.dev.Close()
	h.mu.Unlock()
	h.workers.Wait()
	return 0
}

// Return success when a retry worker is accepted (or already active), not when
// connectivity is restored. stop cancels the worker and waits for its exit.
func (r *handleRegistry) bump(id int32) int64 {
	h := r.lookup(id)
	if h == nil {
		return -int64(unix.EBADF)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return -int64(unix.EBADF)
	}
	if h.bumping {
		return 0
	}
	h.bumping = true
	h.workers.Add(1)
	go h.retryBind()
	return 0
}

func (h *tunnelHandle) retryBind() {
	defer h.workers.Done()
	defer func() { h.mu.Lock(); h.bumping = false; h.mu.Unlock() }()
	for attempt := 0; attempt < 10; attempt++ {
		h.mu.Lock()
		if h.closed {
			h.mu.Unlock()
			return
		}
		err := h.dev.BindUpdate()
		if err == nil {
			h.dev.SendKeepalivesToPeersWithCurrentKeypair()
		}
		if err != nil {
			h.logger.Errorf("Unable to update bind, try %d: %v", attempt+1, err)
		}
		h.mu.Unlock()
		if err == nil {
			return
		}
		if attempt == 9 {
			break
		}
		timer := time.NewTimer(time.Second / 2)
		select {
		case <-h.done:
			timer.Stop()
			return
		case <-timer.C:
		}
	}
	h.logger.Errorf("Gave up trying to update bind; tunnel is likely dysfunctional")
}
