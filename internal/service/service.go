package service

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/kardianos/service"
	"helium-drm-fixer/internal/detector"
	"helium-drm-fixer/internal/fixer"
	"helium-drm-fixer/internal/memory"
)

const (
	// ServiceName is the system identifier for the service.
	ServiceName = "HeliumDRMFixer"
	// ServiceDisplayName is the human-readable display name.
	ServiceDisplayName = "Helium Browser DRM Fixer Service"
	// ServiceDescription explains the service purpose.
	ServiceDescription = "Monitors and automatically maintains Widevine DRM components for Helium Browser."
)

// Options holds operational configuration for the background service.
type Options struct {
	Interval      time.Duration
	HeliumPath    string
	ChromePath    string
	ForceDownload bool
	Verbose       bool
}

// program implements service.Interface for kardianos/service.
type program struct {
	service service.Service
	logger  service.Logger
	opts    Options
	stopCh  chan struct{}
}

// Start begins background monitoring when triggered by the OS service manager.
func (p *program) Start(s service.Service) error {
	p.stopCh = make(chan struct{})
	go p.run()
	return nil
}

// Stop signals the monitoring loop to terminate cleanly.
func (p *program) Stop(s service.Service) error {
	if p.stopCh != nil {
		close(p.stopCh)
	}
	return nil
}

func (p *program) run() {
	// Set memory limit soft-target to encourage prompt scavenging
	debug.SetMemoryLimit(8 * 1024 * 1024)

	if p.logger != nil {
		_ = p.logger.Infof("Helium DRM Fixer service started (check interval: %v)", p.opts.Interval)
	}

	// Initial check on service launch
	p.checkAndFix()
	memory.Trim()

	ticker := time.NewTicker(p.opts.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			if p.logger != nil {
				_ = p.logger.Info("Helium DRM Fixer service stopped")
			}
			return
		case <-ticker.C:
			p.checkAndFix()
			memory.Trim()
		}
	}
}

func (p *program) checkAndFix() {
	fixOpts := fixer.Options{
		Verbose:       p.opts.Verbose,
		ChromePath:    p.opts.ChromePath,
		HeliumPath:    p.opts.HeliumPath,
		ForceDownload: p.opts.ForceDownload,
	}

	needed, reason, err := fixer.IsFixNeeded(fixOpts)
	if err != nil {
		if p.logger != nil {
			_ = p.logger.Warningf("DRM check warning: %v", err)
		}
		return
	}

	if !needed {
		// Helium already has Widevine in its active version folder
		return
	}

	if p.logger != nil {
		_ = p.logger.Infof("DRM fix needed (%s). Applying fix...", reason)
	}

	if err := fixer.Run(fixOpts); err != nil {
		if p.logger != nil {
			_ = p.logger.Errorf("DRM fix failed: %v", err)
		}
	} else {
		if p.logger != nil {
			_ = p.logger.Info("DRM fix successfully applied to Helium")
		}
	}
}

func getServiceConfig(opts Options) (*service.Config, error) {
	var args []string
	args = append(args, "service", "run")
	if opts.Interval > 0 {
		args = append(args, "--interval", opts.Interval.String())
	}
	if opts.HeliumPath != "" {
		args = append(args, "--helium-path", opts.HeliumPath)
	}
	if opts.ChromePath != "" {
		args = append(args, "--chrome-path", opts.ChromePath)
	}
	if opts.ForceDownload {
		args = append(args, "--force-download")
	}

	options := service.KeyValue{
		"DelayedAutoStart":       true,
		"OnFailure":              "restart",
		"OnFailureDelayDuration": "10s",
		"OnFailureResetPeriod":   86400,
	}

	// For Linux and macOS, UserService installs in user context (~/.config/systemd/user or ~/Library/LaunchAgents)
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		options["UserService"] = true
	}

	return &service.Config{
		Name:        ServiceName,
		DisplayName: ServiceDisplayName,
		Description: ServiceDescription,
		Arguments:   args,
		Option:      options,
	}, nil
}

// IsInteractive returns true if running from a user console rather than a service manager.
func IsInteractive() bool {
	return service.Interactive()
}

// RunDaemon runs the service loop within the operating system service manager.
func RunDaemon(opts Options) error {
	if opts.Interval <= 0 {
		opts.Interval = 15 * time.Minute
	}

	svcConfig, err := getServiceConfig(opts)
	if err != nil {
		return err
	}

	prg := &program{opts: opts}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		return fmt.Errorf("failed to initialize service: %w", err)
	}

	logger, err := s.Logger(nil)
	if err == nil {
		prg.logger = logger
	}
	prg.service = s

	return s.Run()
}

// Install registers the service with the operating system service manager.
func Install(opts Options) error {
	if opts.Interval <= 0 {
		opts.Interval = 15 * time.Minute
	}

	// If HeliumPath is not provided, detect and save it now while running in the user session
	if opts.HeliumPath == "" {
		dummyLog := fixer.NewLogger(false)
		if detected, err := detector.FindHeliumBasePath(dummyLog); err == nil && detected != "" {
			opts.HeliumPath = detected
		}
	}

	svcConfig, err := getServiceConfig(opts)
	if err != nil {
		return err
	}

	prg := &program{opts: opts}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		return fmt.Errorf("failed to configure service: %w", err)
	}

	if err := s.Install(); err != nil {
		return fmt.Errorf("failed to install service: %w", err)
	}

	return nil
}

// Uninstall unregisters and removes the service from the system.
func Uninstall() error {
	svcConfig, err := getServiceConfig(Options{})
	if err != nil {
		return err
	}

	prg := &program{}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		return fmt.Errorf("failed to configure service: %w", err)
	}

	// Try stopping first if running
	_ = s.Stop()

	if err := s.Uninstall(); err != nil {
		return fmt.Errorf("failed to uninstall service: %w", err)
	}

	return nil
}

// Start instructs the OS service manager to start the installed service.
func Start() error {
	svcConfig, err := getServiceConfig(Options{})
	if err != nil {
		return err
	}

	prg := &program{}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		return fmt.Errorf("failed to configure service: %w", err)
	}

	if err := s.Start(); err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}

	return nil
}

// Stop instructs the OS service manager to stop the service.
func Stop() error {
	svcConfig, err := getServiceConfig(Options{})
	if err != nil {
		return err
	}

	prg := &program{}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		return fmt.Errorf("failed to configure service: %w", err)
	}

	if err := s.Stop(); err != nil {
		return fmt.Errorf("failed to stop service: %w", err)
	}

	return nil
}

// Restart restarts the service.
func Restart() error {
	svcConfig, err := getServiceConfig(Options{})
	if err != nil {
		return err
	}

	prg := &program{}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		return fmt.Errorf("failed to configure service: %w", err)
	}

	if err := s.Restart(); err != nil {
		return fmt.Errorf("failed to restart service: %w", err)
	}

	return nil
}

// Status queries the OS service manager for the current service status.
func Status() (string, error) {
	svcConfig, err := getServiceConfig(Options{})
	if err != nil {
		return "", err
	}

	prg := &program{}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		return "", err
	}

	status, err := s.Status()
	if err != nil {
		return "Not installed / Unknown", nil
	}

	switch status {
	case service.StatusRunning:
		return "Running", nil
	case service.StatusStopped:
		return "Stopped", nil
	default:
		return "Unknown / Not installed", nil
	}
}
