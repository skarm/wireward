// SPDX-License-Identifier: MIT

package main

// runtime.Stack returns len(buf) when truncated. Never append a NUL in-place
// at that index; C string allocation belongs to the logger boundary.
func captureStacks(stack func([]byte, bool) int) string {
	const limit = 1 << 20
	for size := 4096; ; size *= 2 {
		buf := make([]byte, size)
		n := stack(buf, true)
		if n < len(buf) {
			return string(buf[:n])
		}
		if size == limit {
			return string(buf) + "\n[stack dump truncated]\n"
		}
	}
}
