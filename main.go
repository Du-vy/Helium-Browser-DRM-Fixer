package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"helium-drm-fixer/internal/fixer"
	"helium-drm-fixer/internal/service"
	"helium-drm-fixer/internal/updater"
)

var (
	// Version is overridden at release build time via
	// -ldflags "-X main.Version=x.y.z -X main.BuildDate=yyyy-mm-dd".
	Version   = "1.2.0"
	BuildDate = "2026-10-23"
)

func main() {
	// Enable Virtual Terminal Processing for ANSI colors on Windows console
	fixer.InitConsole()

	// Dispatch subcommands before the non-interactive daemon check below.
	// Schedulers (systemd timers, launchd, cron) run without an interactive
	// parent, but `update --yes`, `version` and `service ...` must still work
	// there. The service daemon itself is launched as `service run`.
	if len(os.Args) > 1 {
		switch strings.ToLower(os.Args[1]) {
		case "service":
			handleServiceCommand(os.Args[2:])
			return
		case "update":
			handleUpdateCommand(os.Args[2:])
			return
		case "version", "-v", "--version":
			fmt.Printf("helium-drm-fixer v%s (%s) %s/%s\n", Version, BuildDate, runtime.GOOS, runtime.GOARCH)
			return
		}
	}

	// If executed by the OS Service Control Manager (non-interactive daemon),
	// run silently. This is the fallback for daemon launches that do not use
	// the `service run` subcommand.
	if !service.IsInteractive() {
		opts := parseServiceArgs(os.Args[1:])
		if err := service.RunDaemon(opts); err != nil {
			fmt.Fprintf(os.Stderr, "Service daemon error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Interactive CLI mode
	printBanner()

	flag.Usage = printHelp
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

func printBanner() {
	fmt.Printf("%s%s", fixer.ColorCyan, fixer.ColorBold)
	fmt.Println(`
  _    _      _ _                 _____  _____  __  __ 
 | |  | |    | (_)               |  __ \|  __ \|  \/  |
 | |__| | ___| |_ _   _ _ __ ___ | |  | | |__) | |\/| |
 |  __  |/ _ \ | | | | | '_ ` + "`" + ` _ \| |  | |  _  /| |  | |
 | |  | |  __/ | | |_| | | | | | | |__| | | \ \| |  | |
 |_|  |_|\___|_|_|\__,_|_| |_| |_|_____/|_|  \_\_|  |_|
                   Helium Browser DRM Fixer`)
	fmt.Printf("%s  v%s (%s) | %s/%s | %s\n\n", fixer.ColorDim, Version, BuildDate, runtime.GOOS, runtime.GOARCH, getElevationString())
	fmt.Print(fixer.ColorReset)
}

func getElevationString() string {
	if fixer.IsElevated() {
		return "Elevated (Admin)"
	}
	return "Standard User"
}

func handleServiceCommand(args []string) {
	if len(args) < 1 {
		printBanner()
		printServiceUsage()
		os.Exit(1)
	}

	action := strings.ToLower(args[0])

	// Administrative privilege guard for Windows SCM management
	if (action == "install" || action == "uninstall" || action == "start" || action == "stop" || action == "restart") && runtime.GOOS == "windows" {
		if !fixer.IsElevated() {
			printBanner()
			fmt.Fprintf(os.Stderr, "%s[!] Administrative privileges required to manage Windows Services.%s\n", fixer.ColorRed, fixer.ColorReset)
			fmt.Fprintln(os.Stderr, "    Please open PowerShell or Command Prompt as Administrator and run the command again.")
			os.Exit(1)
		}
	}

	switch action {
	case "install":
		opts := parseServiceInstallFlags(args[1:])
		installedPath, err := service.Install(opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%sError installing service:%s %v\n", fixer.ColorRed, fixer.ColorReset, err)
			os.Exit(1)
		}
		fmt.Printf("%s✓ Service '%s' installed successfully.%s\n", fixer.ColorGreen, service.ServiceName, fixer.ColorReset)
		fmt.Printf("  Installed executable: %s\n", installedPath)
		fmt.Println("  The downloaded original is no longer needed (unless it is the installed copy).")
		prefix := ""
		if runtime.GOOS == "windows" {
			prefix = "& " // PowerShell call operator for a quoted executable path.
		}
		fmt.Printf("  Start:  %s\"%s\" service start\n", prefix, installedPath)
		fmt.Printf("  Update: %s\"%s\" update\n", prefix, installedPath)

	case "uninstall":
		if err := service.Uninstall(); err != nil {
			fmt.Fprintf(os.Stderr, "%sError uninstalling service:%s %v\n", fixer.ColorRed, fixer.ColorReset, err)
			os.Exit(1)
		}
		fmt.Printf("%s✓ Service '%s' uninstalled successfully.%s\n", fixer.ColorGreen, service.ServiceName, fixer.ColorReset)

	case "start":
		if err := service.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "%sError starting service:%s %v\n", fixer.ColorRed, fixer.ColorReset, err)
			os.Exit(1)
		}
		fmt.Printf("%s✓ Service '%s' started.%s\n", fixer.ColorGreen, service.ServiceName, fixer.ColorReset)

	case "stop":
		if err := service.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "%sError stopping service:%s %v\n", fixer.ColorRed, fixer.ColorReset, err)
			os.Exit(1)
		}
		fmt.Printf("%s✓ Service '%s' stopped.%s\n", fixer.ColorGreen, service.ServiceName, fixer.ColorReset)

	case "restart":
		if err := service.Restart(); err != nil {
			fmt.Fprintf(os.Stderr, "%sError restarting service:%s %v\n", fixer.ColorRed, fixer.ColorReset, err)
			os.Exit(1)
		}
		fmt.Printf("%s✓ Service '%s' restarted.%s\n", fixer.ColorGreen, service.ServiceName, fixer.ColorReset)

	case "status":
		printBanner()
		st, err := service.Status()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%sError checking service status:%s %v\n", fixer.ColorRed, fixer.ColorReset, err)
			os.Exit(1)
		}
		fmt.Printf("Service '%s': %s%s%s\n\n", service.ServiceName, fixer.ColorBold, st, fixer.ColorReset)

	case "run":
		opts := parseServiceArgs(args[1:])
		if err := service.RunDaemon(opts); err != nil {
			fmt.Fprintf(os.Stderr, "%sError running service daemon:%s %v\n", fixer.ColorRed, fixer.ColorReset, err)
			os.Exit(1)
		}

	case "help", "-h", "--help":
		printBanner()
		printServiceUsage()

	default:
		printBanner()
		fmt.Fprintf(os.Stderr, "%sUnknown service action '%s'%s\n\n", fixer.ColorRed, action, fixer.ColorReset)
		printServiceUsage()
		os.Exit(1)
	}
}

func parseServiceInstallFlags(args []string) service.Options {
	for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		args = args[1:]
	}
	fs := flag.NewFlagSet("service install", flag.ExitOnError)
	installDir := fs.String("install-dir", "", "Permanent directory for the service executable (default: platform-specific)")
	interval := fs.Duration("interval", 15*time.Minute, "Periodic inspection interval (e.g. 15m, 1h)")
	chromePath := fs.String("chrome-path", "", "Custom Chrome WidevineCdm path fallback")
	heliumPath := fs.String("helium-path", "", "Custom Helium application path")
	forceDownload := fs.Bool("force-download", false, "Force re-download Widevine even if cached")
	verbose := fs.Bool("verbose", false, "Enable verbose logging")
	_ = fs.Parse(args)

	return service.Options{
		InstallDir:    *installDir,
		Interval:      *interval,
		HeliumPath:    *heliumPath,
		ChromePath:    *chromePath,
		ForceDownload: *forceDownload,
		Verbose:       *verbose,
	}
}

func parseServiceArgs(args []string) service.Options {
	for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		args = args[1:]
	}
	fs := flag.NewFlagSet("service run", flag.ContinueOnError)
	interval := fs.Duration("interval", 15*time.Minute, "Periodic inspection interval (e.g. 15m, 1h)")
	chromePath := fs.String("chrome-path", "", "Custom Chrome WidevineCdm path fallback")
	heliumPath := fs.String("helium-path", "", "Custom Helium application path")
	forceDownload := fs.Bool("force-download", false, "Force re-download Widevine even if cached")
	verbose := fs.Bool("verbose", false, "Enable verbose logging")
	_ = fs.Parse(args)

	return service.Options{
		Interval:      *interval,
		HeliumPath:    *heliumPath,
		ChromePath:    *chromePath,
		ForceDownload: *forceDownload,
		Verbose:       *verbose,
	}
}

func printHelp() {
	fmt.Println("Usage:")
	fmt.Println("  helium-drm-fixer [options]             # Run DRM fix directly (one-shot)")
	fmt.Println("  helium-drm-fixer service <action>     # Manage background service")
	fmt.Println("  helium-drm-fixer update [options]     # Update this binary from GitHub releases")
	fmt.Println("  helium-drm-fixer version              # Print the installed version")
	fmt.Println()
	fmt.Println("Options:")
	flag.PrintDefaults()
	fmt.Println()
	fmt.Println("Service Commands:")
	fmt.Println("  helium-drm-fixer service install [--interval 15m]   # Install automated service")
	fmt.Println("  helium-drm-fixer service start                      # Start background service")
	fmt.Println("  helium-drm-fixer service stop                       # Stop background service")
	fmt.Println("  helium-drm-fixer service status                     # View service status")
	fmt.Println("  helium-drm-fixer service uninstall                  # Uninstall service")
	fmt.Println()
	fmt.Println("Update Commands:")
	fmt.Println("  helium-drm-fixer update                       # Check for the latest release and install it")
	fmt.Println("  helium-drm-fixer update --check               # Only check availability, install nothing")
	fmt.Println("  helium-drm-fixer update --yes                 # Install without confirmation prompt")
	fmt.Println("  helium-drm-fixer update --force               # Reinstall even if the version is current")
	fmt.Println("  helium-drm-fixer update --skip-verify         # Install without SHA-256 verification")
	fmt.Println("  helium-drm-fixer update --repo <owner/name>   # Use a different GitHub repository")
	fmt.Println()
}

// handleUpdateCommand keeps the installed binary current with the latest
// GitHub release and restarts the background service when it was running.
func handleUpdateCommand(args []string) {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	fs.Usage = func() {
		printBanner()
		fmt.Println("Usage:")
		fmt.Println("  helium-drm-fixer update [options]")
		fmt.Println()
		fmt.Println("Options:")
		fmt.Println("  --check              Only check for a newer release without updating")
		fmt.Println("  --yes                Skip the interactive confirmation prompt")
		fmt.Println("  --force              Update even when the installed version is current")
		fmt.Println("  --skip-verify        Install without verifying the release SHA-256 checksum")
		fmt.Println("  --repo <owner/name>  GitHub repository to fetch releases from (default: " + updater.DefaultRepo + ")")
		fmt.Println()
	}
	checkOnly := fs.Bool("check", false, "Only check for a newer release without updating")
	yes := fs.Bool("yes", false, "Skip the interactive confirmation prompt")
	force := fs.Bool("force", false, "Update even when the installed version is current")
	skipVerify := fs.Bool("skip-verify", false, "Install without verifying the release SHA-256 checksum")
	repo := fs.String("repo", updater.DefaultRepo, "GitHub repository (owner/name) to fetch releases from")
	_ = fs.Parse(args)

	printBanner()

	res, err := updater.Run(updater.Options{
		Repo:       *repo,
		Current:    Version,
		Force:      *force,
		CheckOnly:  *checkOnly,
		AssumeYes:  *yes,
		SkipVerify: *skipVerify,
		BeforeInstall: func(*updater.Result) error {
			return checkUpdatePrivileges()
		},
	})

	if err != nil && !errors.Is(err, updater.ErrAborted) {
		fmt.Fprintf(os.Stderr, "%sError:%s %v\n", fixer.ColorRed, fixer.ColorReset, err)
		os.Exit(1)
	}

	if res == nil {
		return
	}
	printUpdateOutcome(res, *checkOnly, errors.Is(err, updater.ErrAborted))

	if res.Warning != "" {
		fmt.Fprintf(os.Stderr, "%s[!] %s%s\n", fixer.ColorYellow, res.Warning, fixer.ColorReset)
		os.Exit(1)
	}
}

// checkUpdatePrivileges requests elevation only when an update is actually
// available. On Windows the installed service binary can only be replaced from
// an elevated terminal; on Unix no elevation is required.
func checkUpdatePrivileges() error {
	if runtime.GOOS != "windows" || fixer.IsElevated() {
		return nil
	}

	installed, err := service.IsInstalled()
	if err != nil {
		return fmt.Errorf("could not check whether the background service is installed: %w", err)
	}
	if installed {
		return errors.New("administrative privileges required to replace the service binary; open PowerShell or Command Prompt as Administrator and run the command again")
	}

	return nil
}

func printUpdateOutcome(res *updater.Result, checkOnly, aborted bool) {
	if aborted {
		fmt.Printf("%s✗ Update cancelled.%s Nothing was changed.\n", fixer.ColorRed, fixer.ColorReset)
		return
	}

	if !res.UpdateAvailable {
		fmt.Printf("%s✓ Up to date.%s v%s is the latest release (running %s).\n", fixer.ColorGreen, fixer.ColorReset, res.To, updateVersionLabel(res.From))
		return
	}

	if checkOnly {
		fmt.Printf("%s• Update available:%s %s → v%s\n", fixer.ColorYellow, fixer.ColorReset, updateVersionLabel(res.From), res.To)
		fmt.Printf("  Asset: %s (%.1f MB)\n", res.AssetName, float64(res.AssetSize)/(1024*1024))
		fmt.Println("  Run 'helium-drm-fixer update' to install it.")
		return
	}

	if res.Updated {
		if res.ChecksumVerified {
			fmt.Printf("%s✓ Checksum verified.%s\n", fixer.ColorGreen, fixer.ColorReset)
		}
		fmt.Printf("%s✓ Updated to v%s.%s\n", fixer.ColorGreen, res.To, fixer.ColorReset)
		if res.BackupPath != "" {
			fmt.Printf("  Previous binary kept at %s\n", res.BackupPath)
		}
		if res.ServiceRestarted {
			fmt.Printf("  Background service restarted with the new version.\n")
		}
	}
}

func updateVersionLabel(v string) string {
	if v == "" {
		return "unknown"
	}
	return "v" + v
}

func printServiceUsage() {
	fmt.Println("Usage:")
	fmt.Println("  helium-drm-fixer service <action> [options]")
	fmt.Println()
	fmt.Println("Actions:")
	fmt.Println("  install     Copy to a permanent directory and register the service")
	fmt.Println("  uninstall   Remove the registered service")
	fmt.Println("  start       Start the service")
	fmt.Println("  stop        Stop the service")
	fmt.Println("  restart     Restart the service")
	fmt.Println("  status      Check current service status")
	fmt.Println("  run         Run daemon directly in the foreground (debug/testing)")
	fmt.Println()
	fmt.Println("Options for 'install':")
	fmt.Println("  --install-dir <path>    Permanent directory for the service executable")
	fmt.Println("  --interval <duration>   Check interval (default: 15m, e.g. 5m, 30m, 1h)")
	fmt.Println("  --helium-path <path>    Custom Helium application path")
	fmt.Println("  --chrome-path <path>    Custom Chrome fallback path")
	fmt.Println("  --force-download        Force re-download Widevine even if cached")
	fmt.Println("  --verbose               Enable verbose logging")
	fmt.Println()
}
