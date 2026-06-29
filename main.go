package main

import (
	"flag"
	"fmt"
	"os"
)

type fixOptions struct {
	verbose       bool
	dryRun        bool
	check         bool
	chromePath    string
	heliumPath    string
	forceDownload bool
}

func main() {
	check := flag.Bool("check", false, "Check if fix is needed without applying changes")
	dryRun := flag.Bool("dry-run", false, "Show what would be done without making changes")
	verbose := flag.Bool("verbose", false, "Enable verbose logging")
	debug := flag.Bool("debug", false, "Enable debug logging (alias for --verbose)")
	chromePath := flag.String("chrome-path", "", "Custom Chrome WidevineCdm path")
	heliumPath := flag.String("helium-path", "", "Custom Helium WidevineCdm path")
	forceDownload := flag.Bool("force-download", false, "Force re-download Chrome even if cached")
	flag.Parse()

	opts := fixOptions{
		verbose:       *verbose || *debug,
		dryRun:        *dryRun,
		check:         *check,
		chromePath:    *chromePath,
		heliumPath:    *heliumPath,
		forceDownload: *forceDownload,
	}

	if err := fixHeliumDRM(opts); err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %v\n", colorRed, colorReset, err)
		os.Exit(1)
	}
}
