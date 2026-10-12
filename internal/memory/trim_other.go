//go:build !windows
// +build !windows

package memory

import (
	"runtime"
	"runtime/debug"
)

// Trim aggressively returns unused memory to the operating system.
func Trim() {
	runtime.GC()
	debug.FreeOSMemory()
}
