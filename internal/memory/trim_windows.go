//go:build windows
// +build windows

package memory

import (
	"runtime"
	"runtime/debug"

	"golang.org/x/sys/windows"
)

var (
	kernel32                 = windows.NewLazySystemDLL("kernel32.dll")
	procSetProcessWorkingSet = kernel32.NewProc("SetProcessWorkingSetSize")
)

// Trim aggressively frees unused memory from the Go runtime and flushes the Windows working set.
func Trim() {
	runtime.GC()
	debug.FreeOSMemory()
	handle := windows.CurrentProcess()
	_, _, _ = procSetProcessWorkingSet.Call(uintptr(handle), ^uintptr(0), ^uintptr(0))
}
