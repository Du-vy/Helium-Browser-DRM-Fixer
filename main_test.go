package main

import (
	"testing"
	"time"
)

func TestParseServiceInstallFlags(t *testing.T) {
	opts := parseServiceInstallFlags([]string{
		"--install-dir", "custom permanent directory",
		"--interval", "30m",
		"--helium-path", "helium path",
		"--chrome-path", "chrome path",
		"--force-download", "--verbose",
	})
	if opts.InstallDir != "custom permanent directory" || opts.Interval != 30*time.Minute ||
		opts.HeliumPath != "helium path" || opts.ChromePath != "chrome path" ||
		!opts.ForceDownload || !opts.Verbose {
		t.Fatalf("unexpected options: %+v", opts)
	}
}

func TestParseServiceInstallDefaults(t *testing.T) {
	opts := parseServiceInstallFlags(nil)
	if opts.InstallDir != "" || opts.Interval != 15*time.Minute {
		t.Fatalf("unexpected defaults: %+v", opts)
	}
}
