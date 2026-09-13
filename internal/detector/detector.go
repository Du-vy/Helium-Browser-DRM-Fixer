package detector

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
)

// Logger defines logging methods expected by detector.
type Logger interface {
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
}

var versionDirPattern = regexp.MustCompile(`^\d+\.\d+\.\d+\.\d+$`)

// FindChromeWidevinePath scans the local operating system to locate Chrome's WidevineCdm directory.
func FindChromeWidevinePath(log Logger) (string, error) {
	chromeExe, err := findChromeExe()
	if err != nil {
		return "", err
	}
	log.Info("Found Chrome executable at: %s", chromeExe)

	switch runtime.GOOS {
	case "windows":
		return findChromeWidevineWindows(filepath.Dir(chromeExe), log)
	case "darwin":
		return findChromeWidevineDarwin(chromeExe, log)
	case "linux":
		return findChromeWidevineLinux(chromeExe, log)
	default:
		return "", fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

func findChromeExe() (string, error) {
	switch runtime.GOOS {
	case "windows":
		return findChromeExeWindows()
	case "darwin":
		return findChromeExeDarwin()
	case "linux":
		return findChromeExeLinux()
	}
	return "", fmt.Errorf("unsupported platform: %s", runtime.GOOS)
}

func findChromeExeWindows() (string, error) {
	candidates := []string{
		filepath.Join(os.Getenv("ProgramFiles"), "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Google", "Chrome", "Application", "chrome.exe"),
	}

	candidates = append(candidates, registryChromePaths()...)

	for _, p := range candidates {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", errors.New("Chrome not found. Please install Chrome or use --chrome-path")
}

func findChromeExeDarwin() (string, error) {
	p := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	return "", errors.New("Chrome not found. Please install Chrome or use --chrome-path")
}

func findChromeExeLinux() (string, error) {
	bins := []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "google-chrome-beta"}

	for _, name := range bins {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}

	knownPaths := []string{
		"/opt/google/chrome/chrome",
		"/opt/google/chrome-beta/chrome",
		"/usr/lib/chromium/chromium",
		"/usr/lib/chromium-browser/chromium",
	}

	for _, p := range knownPaths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", errors.New("Chrome not found. Please install Chrome or use --chrome-path")
}

func findChromeWidevineWindows(chromeDir string, log Logger) (string, error) {
	log.Info("Scanning Chrome Application directory: %s", chromeDir)

	entries, err := os.ReadDir(chromeDir)
	if err != nil {
		return "", err
	}

	for _, entry := range entries {
		if !entry.IsDir() || !versionDirPattern.MatchString(entry.Name()) {
			continue
		}
		versionPath := filepath.Join(chromeDir, entry.Name(), "WidevineCdm")
		log.Info("Checking path: %s", versionPath)
		if info, err := os.Stat(versionPath); err == nil && info.IsDir() {
			log.Info("Found WidevineCdm at: %s", versionPath)
			return versionPath, nil
		}
		log.Info("Path does not exist: %s", versionPath)
	}

	return "", errors.New("WidevineCdm not found in Chrome installation")
}

func findChromeWidevineDarwin(chromeExe string, log Logger) (string, error) {
	versionsPath := filepath.Join(
		filepath.Dir(chromeExe),
		"..",
		"Frameworks",
		"Google Chrome Framework.framework",
		"Versions",
	)

	log.Info("Scanning Chrome Versions directory: %s", versionsPath)

	entries, err := os.ReadDir(versionsPath)
	if err != nil {
		return "", err
	}

	for _, entry := range entries {
		if !entry.IsDir() || !versionDirPattern.MatchString(entry.Name()) {
			continue
		}
		versionPath := filepath.Join(versionsPath, entry.Name(), "Libraries", "WidevineCdm")
		log.Info("Checking path: %s", versionPath)
		if info, err := os.Stat(versionPath); err == nil && info.IsDir() {
			log.Info("Found WidevineCdm at: %s", versionPath)
			return versionPath, nil
		}
		log.Info("Path does not exist: %s", versionPath)
	}

	return "", errors.New("WidevineCdm not found in Chrome installation")
}

func findChromeWidevineLinux(chromeExe string, log Logger) (string, error) {
	chromeDir := filepath.Dir(chromeExe)

	if resolved, err := filepath.EvalSymlinks(chromeExe); err == nil && resolved != chromeExe {
		chromeDir = filepath.Dir(resolved)
		log.Info("Resolved Chrome symlink to: %s", chromeDir)
	}

	directPath := filepath.Join(chromeDir, "WidevineCdm")
	log.Info("Checking direct path: %s", directPath)
	if info, err := os.Stat(directPath); err == nil && info.IsDir() {
		log.Info("Found WidevineCdm at: %s", directPath)
		return directPath, nil
	}

	log.Info("Direct path does not exist, scanning for version folders in: %s", chromeDir)
	entries, err := os.ReadDir(chromeDir)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() || !versionDirPattern.MatchString(entry.Name()) {
				continue
			}
			versionPath := filepath.Join(chromeDir, entry.Name(), "WidevineCdm")
			log.Info("Checking path: %s", versionPath)
			if info, err := os.Stat(versionPath); err == nil && info.IsDir() {
				log.Info("Found WidevineCdm at: %s", versionPath)
				return versionPath, nil
			}
		}
	}

	knownPaths := []string{
		"/opt/google/chrome/WidevineCdm",
		"/opt/google/chrome-beta/WidevineCdm",
		"/usr/lib/chromium/WidevineCdm",
		"/usr/lib/chromium-browser/WidevineCdm",
	}
	for _, p := range knownPaths {
		log.Info("Checking known Chrome path: %s", p)
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			log.Info("Found WidevineCdm at: %s", p)
			return p, nil
		}
	}

	return "", errors.New("WidevineCdm not found in Chrome installation")
}

// FindHeliumVersionPath scans standard operating system locations to find the active Helium version folder.
func FindHeliumVersionPath(log Logger) (string, error) {
	var basePath string

	switch runtime.GOOS {
	case "windows":
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			home, _ := os.UserHomeDir()
			localAppData = filepath.Join(home, "AppData", "Local")
		}
		basePath = filepath.Join(localAppData, "imput", "Helium", "Application")
	case "darwin":
		basePath = filepath.Join(
			"/Applications",
			"Helium.app",
			"Contents",
			"Frameworks",
			"Helium Framework.framework",
			"Versions",
		)
	case "linux":
		sysPath, err := findHeliumLinuxInstall(log)
		if err == nil {
			return sysPath, nil
		}
		home, _ := os.UserHomeDir()
		basePath = filepath.Join(home, ".config", "Helium", "Application")
	}

	log.Info("Checking Helium base path: %s", basePath)

	info, err := os.Stat(basePath)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("helium base path not found: %s", basePath)
	}

	log.Info("Scanning for Helium version folders in: %s", basePath)
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return "", err
	}

	for _, entry := range entries {
		if !entry.IsDir() || !versionDirPattern.MatchString(entry.Name()) {
			continue
		}
		if runtime.GOOS == "darwin" {
			versionPath := filepath.Join(basePath, entry.Name(), "Libraries")
			log.Info("Found Helium version folder: %s", versionPath)
			return versionPath, nil
		}
		versionPath := filepath.Join(basePath, entry.Name())
		log.Info("Found Helium version folder: %s", versionPath)
		return versionPath, nil
	}

	return "", errors.New("no valid Helium version folder found")
}

func findHeliumLinuxInstall(log Logger) (string, error) {
	var candidates []string

	for _, bin := range []string{"/usr/bin/helium-browser", "/usr/bin/helium"} {
		resolved, err := filepath.EvalSymlinks(bin)
		if err != nil {
			continue
		}
		dir := filepath.Dir(resolved)
		candidates = append(candidates, dir)
	}

	candidates = append(candidates, "/opt/helium-browser-bin")

	for _, candidate := range candidates {
		log.Info("Checking Linux Helium system path: %s", candidate)
		info, err := os.Stat(candidate)
		if err != nil || !info.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(candidate, "helium")); err == nil {
			log.Info("Found Helium system installation: %s", candidate)
			return candidate, nil
		}
	}

	return "", errors.New("helium system installation not found")
}

// FindHeliumUserDataDir returns the configuration/user data directory for Helium on Linux.
func FindHeliumUserDataDir(log Logger) (string, error) {
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".config", "net.imput.helium"),
		filepath.Join(home, ".config", "Helium"),
		filepath.Join(home, ".config", "helium"),
	}

	for _, dir := range candidates {
		log.Info("Checking Helium user data dir: %s", dir)
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			log.Info("Found Helium user data dir: %s", dir)
			return dir, nil
		}
	}

	return "", errors.New("could not find Helium user data directory")
}

// ReadWidevineVersion parses manifest.json in the Widevine directory to extract its version string.
func ReadWidevineVersion(widevinePath string) (string, error) {
	data, err := os.ReadFile(filepath.Join(widevinePath, "manifest.json"))
	if err != nil {
		return "", fmt.Errorf("failed to read manifest.json: %w", err)
	}

	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", fmt.Errorf("failed to parse manifest.json: %w", err)
	}
	if manifest.Version == "" {
		return "", errors.New("version not found in manifest.json")
	}
	return manifest.Version, nil
}
