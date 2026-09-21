// SPDX-License-Identifier: MIT
package main

import (
	"errors"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"golang.zx2c4.com/wireguard/device"
)

type fakeDevice struct {
	setErr, upErr, bindErr   error
	closes, ups, sets, binds atomic.Int32
	closed                   atomic.Bool
	entered, release         chan struct{}
}

func (d *fakeDevice) check() {
	if d.closed.Load() {
		panic("operation after Close")
	}
}
func (d *fakeDevice) IpcSet(string) error     { d.check(); d.sets.Add(1); return d.setErr }
func (d *fakeDevice) IpcGet() (string, error) { d.check(); return "private_key=test\n", nil }
func (d *fakeDevice) Up() error               { d.check(); d.ups.Add(1); return d.upErr }
func (d *fakeDevice) Close()                  { d.closed.Store(true); d.closes.Add(1) }
func (d *fakeDevice) BindUpdate() error {
	d.check()
	d.binds.Add(1)
	if d.entered != nil {
		close(d.entered)
		<-d.release
	}
	return d.bindErr
}
func (d *fakeDevice) SendKeepalivesToPeersWithCurrentKeypair()    { d.check() }
func (d *fakeDevice) DisableSomeRoamingForBrokenMobileSemantics() { d.check() }
func quietLogger() *device.Logger {
	return &device.Logger{Verbosef: func(string, ...any) {}, Errorf: func(string, ...any) {}}
}
func registry() *handleRegistry { return &handleRegistry{handles: make(map[int32]*tunnelHandle)} }

func TestStartFailuresCloseDevice(t *testing.T) {
	for _, test := range []struct {
		name          string
		setErr, upErr error
		next          int64
		want          int32
		ups           int32
	}{
		{name: "IPC", setErr: unix.EINVAL, want: -int32(unix.EINVAL)},
		{name: "Up", upErr: unix.EADDRINUSE, want: -int32(unix.EADDRINUSE), ups: 1},
		{name: "handles exhausted", next: math.MaxInt32 + 1, want: -int32(unix.EMFILE), ups: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := registry()
			r.next = test.next
			d := &fakeDevice{setErr: test.setErr, upErr: test.upErr}
			if got := r.start(d, quietLogger(), ""); got != test.want {
				t.Fatalf("start=%d want %d", got, test.want)
			}
			if d.closes.Load() != 1 || d.ups.Load() != test.ups || len(r.handles) != 0 {
				t.Fatal("failed start leaked or published a device")
			}
		})
	}
}

func TestStaleHandlesNeverAffectNewDevice(t *testing.T) {
	r := registry()
	first := &fakeDevice{}
	old := r.start(first, quietLogger(), "")
	if code := r.stop(old); code != 0 {
		t.Fatal(code)
	}
	second := &fakeDevice{}
	id := r.start(second, quietLogger(), "")
	defer r.stop(id)
	if old == id {
		t.Fatal("handle reused")
	}
	if r.stop(old) != -int64(unix.EBADF) || r.bump(old) != -int64(unix.EBADF) {
		t.Fatal("stale handle accepted")
	}
	if r.withDevice(old, func(d backendDevice) error { return d.IpcSet("") }) != -int64(unix.EBADF) {
		t.Fatal("stale update accepted")
	}
	if second.closes.Load() != 0 || first.closes.Load() != 1 {
		t.Fatal("wrong device closed")
	}
}

func TestConcurrentOperationsAndStop(t *testing.T) {
	r := registry()
	d := &fakeDevice{}
	id := r.start(d, quietLogger(), "")
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 30; j++ {
				code := r.withDevice(id, func(d backendDevice) error { return d.IpcSet("") })
				if code != 0 && code != -int64(unix.EBADF) {
					t.Errorf("status %d", code)
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if r.stop(id) != 0 {
			t.Error("stop failed")
		}
	}()
	wg.Wait()
	if d.closes.Load() != 1 {
		t.Fatal("Close not called exactly once")
	}
}

func TestStopWaitsForActiveBindAndCancelsRetry(t *testing.T) {
	r := registry()
	d := &fakeDevice{bindErr: unix.EADDRINUSE, entered: make(chan struct{}), release: make(chan struct{})}
	id := r.start(d, quietLogger(), "")
	if r.bump(id) != 0 {
		t.Fatal("bump failed")
	}
	select {
	case <-d.entered:
	case <-time.After(time.Second):
		t.Fatal("bind did not start")
	}
	stopped := make(chan int64, 1)
	go func() { stopped <- r.stop(id) }()
	deadline := time.Now().Add(time.Second)
	for r.lookup(id) != nil {
		if time.Now().After(deadline) {
			t.Fatal("stop not scheduled")
		}
		time.Sleep(time.Millisecond)
	}
	if d.closes.Load() != 0 {
		t.Fatal("closed while BindUpdate was active")
	}
	close(d.release)
	select {
	case code := <-stopped:
		if code != 0 {
			t.Fatal(code)
		}
	case <-time.After(time.Second):
		t.Fatal("stop did not cancel retry")
	}
	if d.binds.Load() != 1 || d.closes.Load() != 1 {
		t.Fatal("retry survived stop")
	}
}

func TestBumpCoalescesPendingRetries(t *testing.T) {
	r := registry()
	d := &fakeDevice{bindErr: unix.EADDRINUSE}
	id := r.start(d, quietLogger(), "")
	for i := 0; i < 100; i++ {
		if r.bump(id) != 0 {
			t.Fatal("bump failed")
		}
	}
	if code := r.stop(id); code != 0 {
		t.Fatal(code)
	}
	if d.binds.Load() > 1 {
		t.Fatalf("created redundant workers: %d", d.binds.Load())
	}
}

func TestErrorCode(t *testing.T) {
	if errorCode(errors.New("failure")) != -int64(unix.EIO) {
		t.Fatal("unknown error became success")
	}
	if errorCode(nil) != 0 {
		t.Fatal("nil error failed")
	}
}

func TestStackCaptureFullBufferAndLimit(t *testing.T) {
	for _, size := range []int{0, 4095, 4096, 4097, 1 << 20, (1 << 20) + 1} {
		got := captureStacks(func(b []byte, _ bool) int {
			n := min(len(b), size)
			for i := 0; i < n; i++ {
				b[i] = 'x'
			}
			return n
		})
		if size < 1<<20 {
			if len(got) != size {
				t.Fatalf("size %d: length %d", size, len(got))
			}
		} else if !strings.HasSuffix(got, "[stack dump truncated]\n") {
			t.Fatal("unbounded or unmarked truncation")
		}
	}
}

func FuzzStackCapture(f *testing.F) {
	for _, seed := range []uint16{0, 4095, 4096, 4097, 65535} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, size uint16) {
		text := captureStacks(func(b []byte, _ bool) int { return min(len(b), int(size)) })
		if len(text) != int(size) {
			t.Fatalf("length %d != %d", len(text), size)
		}
	})
}

func TestLoggerReplacementIsSynchronized(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				wgSetLogger(nil, nil)
				CLogger(0).Printf("test %d", j)
			}
		}()
	}
	wg.Wait()
}
