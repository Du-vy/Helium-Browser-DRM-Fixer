package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"helium-drm-fixer/internal/fixer"
	"helium-drm-fixer/internal/service"
)

var (
	Version   = "1.1.0"
	BuildDate = "2026-10-12"
)

func main() {
	// Enable Virtual Terminal Processing for ANSI colors on Windows console
	fixer.InitConsole()

	// If executed by OS Service Control Manager (non-interactive daemon), run silently
	if !service.IsInteractive() {
		opts := parseServiceArgs(os.Args[1:])
		if err := service.RunDaemon(opts); err != nil {
			fmt.Fprintf(os.Stderr, "Service daemon error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Service management subcommands
	if len(os.Args) > 1 && strings.EqualFold(os.Args[1], "service") {
		handleServiceCommand(os.Args[2:])
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
		if err := service.Install(opts); err != nil {
			fmt.Fprintf(os.Stderr, "%sError installing service:%s %v\n", fixer.ColorRed, fixer.ColorReset, err)
			os.Exit(1)
		}
		fmt.Printf("%s✓ Service '%s' installed successfully.%s\n", fixer.ColorGreen, service.ServiceName, fixer.ColorReset)
		fmt.Println("  To start the service now: helium-drm-fixer service start")

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
}

func printServiceUsage() {
	fmt.Println("Usage:")
	fmt.Println("  helium-drm-fixer service <action> [options]")
	fmt.Println()
	fmt.Println("Actions:")
	fmt.Println("  install     Register as an automated background service")
	fmt.Println("  uninstall   Remove the registered service")
	fmt.Println("  start       Start the service")
	fmt.Println("  stop        Stop the service")
	fmt.Println("  restart     Restart the service")
	fmt.Println("  status      Check current service status")
	fmt.Println("  run         Run daemon directly in the foreground (debug/testing)")
	fmt.Println()
	fmt.Println("Options for 'install':")
	fmt.Println("  --interval <duration>   Check interval (default: 15m, e.g. 5m, 30m, 1h)")
	fmt.Println("  --helium-path <path>    Custom Helium application path")
	fmt.Println("  --chrome-path <path>    Custom Chrome fallback path")
	fmt.Println("  --force-download        Force re-download Widevine even if cached")
	fmt.Println("  --verbose               Enable verbose logging")
	fmt.Println()
}
