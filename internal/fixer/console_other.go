//go:build !windows
// +build !windows

package fixer

import (
	"os"
)

// InitConsole initializes console behavior on Unix-like operating systems.
func InitConsole() {
	if os.Getenv("TERM") == "dumb" || os.Getenv("NO_COLOR") != "" {
		DisableColors()
	}
}

// IsElevated returns true if the current process is running as root (UID 0).
func IsElevated() bool {
	return os.Geteuid() == 0
}
