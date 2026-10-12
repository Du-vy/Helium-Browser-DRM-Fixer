package fixer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"helium-drm-fixer/internal/detector"
	"helium-drm-fixer/internal/widevine"
)

func copyDir(src, dest string) error {
	if err := os.RemoveAll(dest); err != nil {
		return fmt.Errorf("failed to remove destination: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(dest, relPath)

		if d.IsDir() {
			return os.MkdirAll(targetPath, 0755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(targetPath, data, 0644)
	})
}

func resignHeliumApp() error {
	heliumPath := "/Applications/Helium.app"

	cmd := exec.Command("xattr", "-cr", heliumPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to clear extended attributes: %w", err)
	}

	cmd = exec.Command("codesign", "--force", "--deep", "--sign", "-", heliumPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to re-sign Helium.app: %w", err)
	}

	return nil
}

// Run executes the DRM fix workflow according to the provided Options.
func Run(opts Options) error {
	log := NewLogger(opts.Verbose)

	fmt.Printf("%s%s🔧 Fixing Helium DRM...%s\n\n", ColorBold, ColorCyan, ColorReset)

	fmt.Print("Getting WidevineCdm... ")

	var chromeWVPath string
	var err error

	if opts.ChromePath != "" {
		log.Info("Using custom Chrome path: %s", opts.ChromePath)
		chromeWVPath = opts.ChromePath
		fmt.Printf("%s✓%s\n", ColorGreen, ColorReset)
	} else {
		chromeWVPath, err = widevine.Get(log, opts.ForceDownload)
		if err != nil {
			log.Info("Download failed: %v", err)
			fmt.Print("\rDownload failed, looking for local Chrome... ")
			log.Info("Falling back to local Chrome installation...")

			chromeWVPath, err = detector.FindChromeWidevinePath(log)
			if err != nil {
				fmt.Printf("\r%s✗ Could not get WidevineCdm%s\n", ColorRed, ColorReset)
				fmt.Printf("\n%s⚠️  Download failed and no local Chrome found.%s\n", ColorYellow, ColorReset)
				fmt.Printf("%s   Install Chrome manually or specify path with --chrome-path%s\n\n", ColorBlue, ColorReset)
				return fmt.Errorf("could not obtain WidevineCdm: %w", err)
			}
			fmt.Printf("\r%s✓ Found local Chrome at: %s%s\n", ColorGreen, chromeWVPath, ColorReset)
		} else {
			fmt.Printf("%s✓ Downloaded and extracted%s\n", ColorGreen, ColorReset)
		}
	}

	fmt.Print("Looking for Helium installation... ")

	var heliumVersionPath string
	heliumVersionPath, err = detector.ResolveHeliumVersionPath(opts.HeliumPath, log)
	if err != nil {
		fmt.Printf("%s✗%s\n", ColorRed, ColorReset)
		fmt.Printf("\n%s⚠️  Please install Helium first.%s\n", ColorYellow, ColorReset)
		fmt.Printf("%s   Download from: https://helium.computer/%s\n\n", ColorBlue, ColorReset)
		return err
	}
	fmt.Printf("%s✓ Found Helium at: %s%s\n", ColorGreen, heliumVersionPath, ColorReset)

	var heliumWVPath string

	if runtime.GOOS == "linux" {
		manifestVersion, err := detector.ReadWidevineVersion(chromeWVPath)
		if err != nil {
			return fmt.Errorf("failed to read Widevine version: %w", err)
		}

		userDataDir, err := detector.FindHeliumUserDataDir(log)
		if err != nil {
			fmt.Printf("\n%s✗ Could not find Helium user data directory.%s\n", ColorRed, ColorReset)
			fmt.Printf("%s  Please launch Helium at least once before running this tool.%s\n\n", ColorYellow, ColorReset)
			return err
		}

		heliumWVPath = filepath.Join(userDataDir, "WidevineCdm", manifestVersion)
		log.Info("Using Linux component updater path: %s", heliumWVPath)
	} else {
		heliumWVPath = filepath.Join(heliumVersionPath, "WidevineCdm")
	}

	if opts.Check {
		needed, reason, err := IsFixNeeded(opts)
		if err != nil {
			fmt.Printf("\n%s⚠️  Check result: %v%s\n\n", ColorYellow, err, ColorReset)
			return err
		}
		if needed {
			fmt.Printf("\n%s⚠️  Fix is needed: %s%s\n\n", ColorBold+ColorYellow, reason, ColorReset)
		} else {
			fmt.Printf("\n%s✅ DRM is up to date: %s%s\n\n", ColorBold+ColorGreen, reason, ColorReset)
		}
		return nil
	}

	if opts.DryRun {
		fmt.Printf("\n%s🔍 Dry run - no changes made.%s\n", ColorBold+ColorYellow, ColorReset)
		fmt.Printf("%s   Would copy: %s%s\n", ColorDim, chromeWVPath, ColorReset)
		fmt.Printf("%s   To: %s%s\n\n", ColorDim, heliumWVPath, ColorReset)
		return nil
	}

	fmt.Print("Copying WidevineCdm to Helium... ")

	if err := copyDir(chromeWVPath, heliumWVPath); err != nil {
		fmt.Printf("%s✗%s\n", ColorRed, ColorReset)
		return fmt.Errorf("failed to copy WidevineCdm: %w", err)
	}

	fmt.Printf("%s✓%s\n", ColorGreen, ColorReset)

	if runtime.GOOS == "linux" {
		hintFile := filepath.Join(filepath.Dir(heliumWVPath), "latest-component-updated-widevine-cdm")
		hintData := fmt.Sprintf(`{"Path":"%s"}`, heliumWVPath)
		if err := os.WriteFile(hintFile, []byte(hintData), 0644); err != nil {
			log.Warn("Failed to write hint file: %v", err)
		}
		log.Info("Wrote hint file: %s", hintFile)
	}

	if runtime.GOOS == "darwin" {
		fmt.Print("Re-signing Helium.app to include WidevineCdm... ")

		if err := resignHeliumApp(); err != nil {
			fmt.Printf("%s✗%s\n", ColorRed, ColorReset)
			fmt.Printf("\n%s⚠️  You may need to run with sudo, or manually run:%s\n", ColorYellow, ColorReset)
			fmt.Printf("%s   codesign --force --deep --sign - \"/Applications/Helium.app\"%s\n\n", ColorDim, ColorReset)
		} else {
			fmt.Printf("%s✓%s\n", ColorGreen, ColorReset)
		}
	}

	fmt.Printf("\n%s✅ Done! Restart Helium browser for DRM to work.%s\n\n", ColorBold+ColorGreen, ColorReset)
	return nil
}

// IsFixNeeded checks if the Helium installation currently lacks WidevineCdm without applying any changes.
// It returns true if the fix needs to be applied, along with an explanatory status message.
func IsFixNeeded(opts Options) (bool, string, error) {
	log := NewLogger(opts.Verbose)

	heliumVersionPath, err := detector.ResolveHeliumVersionPath(opts.HeliumPath, log)
	if err != nil {
		return false, "", fmt.Errorf("helium installation not found: %w", err)
	}

	if runtime.GOOS == "linux" {
		userDataDir, err := detector.FindHeliumUserDataDir(log)
		if err != nil {
			return true, "Helium user data directory not initialized", nil
		}
		wvBase := filepath.Join(userDataDir, "WidevineCdm")
		entries, err := os.ReadDir(wvBase)
		if err != nil || len(entries) == 0 {
			return true, "WidevineCdm directory missing in Helium user data", nil
		}
		hasManifest := false
		for _, e := range entries {
			if e.IsDir() {
				if _, err := os.Stat(filepath.Join(wvBase, e.Name(), "manifest.json")); err == nil {
					hasManifest = true
					break
				}
			}
		}
		if !hasManifest {
			return true, "manifest.json missing in Helium WidevineCdm", nil
		}
		return false, "Helium already has WidevineCdm configured", nil
	}

	heliumWVPath := filepath.Join(heliumVersionPath, "WidevineCdm")
	manifestPath := filepath.Join(heliumWVPath, "manifest.json")
	if info, err := os.Stat(manifestPath); err != nil || info.IsDir() {
		return true, fmt.Sprintf("WidevineCdm missing in active Helium version (%s)", filepath.Base(heliumVersionPath)), nil
	}

	return false, fmt.Sprintf("WidevineCdm is present in active Helium version (%s)", filepath.Base(heliumVersionPath)), nil
}

