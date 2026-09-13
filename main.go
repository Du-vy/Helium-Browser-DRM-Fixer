package main

import (
	"flag"
	"fmt"
	"os"

	"helium-drm-fixer/internal/fixer"
)

func main() {
	check := flag.Bool("check", false, "Check if fix is needed without applying changes")
	dryRun := flag.Bool("dry-run", false, "Show what would be done without making changes")
	verbose := flag.Bool("verbose", false, "Enable verbose logging")
	debug := flag.Bool("debug", false, "Enable debug logging (alias for --verbose)")
	chromePath := flag.String("chrome-path", "", "Custom Chrome WidevineCdm path")
	heliumPath := flag.String("helium-path", "", "Custom Helium WidevineCdm path")
	forceDownload := flag.Bool("force-download", false, "Force re-download Widevine even if cached")
	flag.Parse()

	opts := fixer.Options{
		Verbose:       *verbose || *debug,
		DryRun:        *dryRun,
		Check:         *check,
		ChromePath:    *chromePath,
		HeliumPath:    *heliumPath,
		ForceDownload: *forceDownload,
	}

	if err := fixer.Run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %v\n", fixer.ColorRed, fixer.ColorReset, err)
		os.Exit(1)
	}
}
