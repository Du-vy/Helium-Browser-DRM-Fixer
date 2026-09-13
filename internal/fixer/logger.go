package fixer

import (
	"fmt"
)

const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorBlue   = "\033[34m"
	ColorCyan   = "\033[36m"
	ColorBold   = "\033[1m"
	ColorDim    = "\033[2m"
)

// Logger provides standard formatted logging across packages.
type Logger interface {
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
}

type stdLogger struct {
	verbose bool
}

func NewLogger(verbose bool) Logger {
	return &stdLogger{verbose: verbose}
}

func (l *stdLogger) Info(msg string, args ...interface{}) {
	if l.verbose {
		fmt.Printf("[INFO] "+msg+"\n", args...)
	}
}

func (l *stdLogger) Warn(msg string, args ...interface{}) {
	if l.verbose {
		fmt.Printf("[WARN] "+msg+"\n", args...)
	}
}

func (l *stdLogger) Error(msg string, args ...interface{}) {
	if l.verbose {
		fmt.Printf("[ERROR] "+msg+"\n", args...)
	}
}
