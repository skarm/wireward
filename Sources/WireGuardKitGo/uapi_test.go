// SPDX-License-Identifier: MIT
package main

import (
	"strings"
	"testing"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/tuntest"
)

func parserDevice() *device.Device {
	tunnel := tuntest.NewChannelTUN().TUN()
	<-tunnel.Events() // Keep the device down; parser tests need no UDP sockets.
	return device.NewDevice(tunnel, conn.NewStdNetBind(), quietLogger())
}

func TestUAPIEmptyReplacementRemovesExistingPeer(t *testing.T) {
	dev := parserDevice()
	defer dev.Close()
	key := strings.Repeat("01", 32)
	if err := dev.IpcSet("public_key=" + key + "\nallowed_ip=10.0.0.0/8\n"); err != nil {
		t.Fatal(err)
	}
	before, err := dev.IpcGet()
	if err != nil || !strings.Contains(before, "public_key=") {
		t.Fatal("peer was not created", err)
	}
	if err := dev.IpcSet("replace_peers=true\n"); err != nil {
		t.Fatal(err)
	}
	after, err := dev.IpcGet()
	if err != nil || strings.Contains(after, "public_key=") {
		t.Fatal("peer survived replacement", err)
	}
}

func FuzzUAPI(f *testing.F) {
	for _, seed := range []string{"", "replace_peers=true\n", "private_key=invalid\n", "public_key=" + strings.Repeat("01", 32) + "\nallowed_ip=::/0\n", "listen_port=65536\n", "unknown=value\n"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 4096 {
			t.Skip()
		}
		dev := parserDevice()
		defer dev.Close()
		// Failed UAPI updates may be partial, but must remain readable and closable.
		_ = dev.IpcSet(input)
		if _, err := dev.IpcGet(); err != nil {
			t.Fatal(err)
		}
	})
}
