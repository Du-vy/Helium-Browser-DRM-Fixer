//go:build windows
// +build windows

package fixer

import (
	"golang.org/x/sys/windows"
)

// InitConsole initializes Windows console to support Virtual Terminal (ANSI escape) sequences.
func InitConsole() {
	stdoutOk := enableVT(windows.Stdout)
	stderrOk := enableVT(windows.Stderr)
	if !stdoutOk && !stderrOk {
		DisableColors()
	}
}

func enableVT(handle windows.Handle) bool {
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err == nil {
		mode |= windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
		if err := windows.SetConsoleMode(handle, mode); err == nil {
			return true
		}
	}
	return false
}

// IsElevated returns true if the current process is running with Administrator privileges.
func IsElevated() bool {
	token := windows.GetCurrentProcessToken()
	return token.IsElevated()
}
