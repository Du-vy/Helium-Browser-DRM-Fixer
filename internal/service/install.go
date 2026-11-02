package service

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/kardianos/service"
)

func defaultInstallDir() (string, error) {
	return platformInstallDir(runtime.GOOS, os.Getenv, os.UserHomeDir)
}

func platformInstallDir(goos string, getenv func(string) string, homeDir func() (string, error)) (string, error) {
	if goos == "windows" {
		// The Windows service runs as LocalSystem: do not place its executable
		// in a user-writable Downloads, AppData or ProgramData directory.
		base := getenv("ProgramFiles")
		if !filepath.IsAbs(base) {
			return "", errors.New("ProgramFiles must be an absolute path; specify --install-dir explicitly")
		}
		return filepath.Join(base, "Helium DRM Fixer"), nil
	}
	if goos == "linux" {
		if base := getenv("XDG_DATA_HOME"); filepath.IsAbs(base) {
			return filepath.Join(base, "helium-drm-fixer", "bin"), nil
		}
	}
	home, err := homeDir()
	if err != nil {
		return "", fmt.Errorf("could not locate the home directory: %w", err)
	}
	switch goos {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "helium-drm-fixer", "bin"), nil
	case "linux":
		return filepath.Join(home, ".local", "share", "helium-drm-fixer", "bin"), nil
	default:
		return "", fmt.Errorf("unsupported OS for service installation: %s", goos)
	}
}

// installFrom keeps file installation testable without touching a real service
// manager. Registration only happens after the copy has been closed successfully.
func installFrom(opts Options, source string, register func(*service.Config) error) (string, error) {
	dir := opts.InstallDir
	if dir == "" {
		var err error
		dir, err = defaultInstallDir()
		if err != nil {
			return "", err
		}
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	name := "helium-drm-fixer"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	destination := filepath.Join(dir, name)

	// Paths supplied from Downloads must not become relative to the service's
	// working directory once its executable moves elsewhere.
	for _, path := range []*string{&opts.HeliumPath, &opts.ChromePath} {
		if *path != "" {
			*path, err = filepath.Abs(*path)
			if err != nil {
				return "", err
			}
		}
	}
	config, err := getServiceConfig(opts)
	if err != nil {
		return "", err
	}
	config.Executable = destination

	created, err := copyExecutable(source, destination)
	if err != nil {
		return "", fmt.Errorf("could not install executable: %w", err)
	}
	if err := register(config); err != nil {
		if created {
			if cleanupErr := os.Remove(destination); cleanupErr != nil {
				return "", fmt.Errorf("failed to register service: %w (could not remove %s: %v)", err, destination, cleanupErr)
			}
		}
		return "", fmt.Errorf("failed to register service: %w", err)
	}
	return destination, nil
}

// copyExecutable never overwrites an existing file. Reinstall from the installed
// copy (or update that copy) instead of risking another binary or a running image.
// The bool reports whether a new file was created and may be removed on failure.
func copyExecutable(source, destination string) (bool, error) {
	in, err := os.Open(source)
	if err != nil {
		return false, err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, errors.New("source executable is not a regular file")
	}
	// Do not accept a symlink back into Downloads as the installed copy.
	if existing, err := os.Lstat(destination); err == nil && os.SameFile(info, existing) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return false, err
	}
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0755)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, fmt.Errorf("%s already exists; run 'service install' from that copy, use its 'update' command, or choose another --install-dir", destination)
		}
		return false, err
	}
	// Ensure executable permissions even when the caller has a restrictive umask.
	_, copyErr := io.Copy(out, in)
	if copyErr == nil {
		copyErr = out.Chmod(0755)
	}
	if copyErr == nil {
		copyErr = out.Sync()
	}
	closeErr := out.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return false, errors.Join(err, os.Remove(destination))
	}
	return true, nil
}
