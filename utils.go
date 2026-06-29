package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorCyan   = "\033[36m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
)

func jsonUnmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

type logger struct {
	verbose bool
}

func (l *logger) info(msg string, args ...interface{}) {
	if l.verbose {
		fmt.Printf("[INFO] "+msg+"\n", args...)
	}
}

func (l *logger) warn(msg string, args ...interface{}) {
	if l.verbose {
		fmt.Printf("[WARN] "+msg+"\n", args...)
	}
}

func (l *logger) error(msg string, args ...interface{}) {
	if l.verbose {
		fmt.Printf("[ERROR] "+msg+"\n", args...)
	}
}

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

func fixHeliumDRM(opts fixOptions) error {
	log := &logger{verbose: opts.verbose}

	fmt.Printf("%s%s🔧 Fixing Helium DRM...%s\n\n", colorBold, colorCyan, colorReset)

	fmt.Print("Getting WidevineCdm... ")

	var chromeWVPath string
	var err error

	if opts.chromePath != "" {
		log.info("Using custom Chrome path: %s", opts.chromePath)
		chromeWVPath = opts.chromePath
		fmt.Printf("%s✓%s\n", colorGreen, colorReset)
	} else {
		chromeWVPath, err = downloadAndExtractChrome(log, opts.forceDownload)
		if err != nil {
			log.info("Download failed: %v", err)
			fmt.Print("\rDownload failed, looking for local Chrome... ")
			log.info("Falling back to local Chrome installation...")

			chromeWVPath, err = findChromeWidevinePath(log)
			if err != nil {
				fmt.Printf("\r%s✗ Could not get WidevineCdm%s\n", colorRed, colorReset)
				fmt.Printf("\n%s⚠️  No Chrome found locally and download failed.%s\n", colorYellow, colorReset)
				if runtime.GOOS != "windows" {
					fmt.Printf("%s   Install Chrome manually or use --chrome-path%s\n", colorBlue, colorReset)
				}
				fmt.Println()
				return fmt.Errorf("could not obtain WidevineCdm: %w", err)
			}
			fmt.Printf("\r%s✓ Found local Chrome at: %s%s\n", colorGreen, chromeWVPath, colorReset)
		} else {
			fmt.Printf("%s✓ Downloaded and extracted%s\n", colorGreen, colorReset)
		}
	}

	fmt.Print("Looking for Helium installation... ")

	var heliumVersionPath string
	if opts.heliumPath != "" {
		log.info("Using custom Helium path: %s", opts.heliumPath)
		heliumVersionPath = opts.heliumPath
		fmt.Printf("%s✓%s\n", colorGreen, colorReset)
	} else {
		heliumVersionPath, err = findHeliumVersionPath(log)
		if err != nil {
			fmt.Printf("%s✗%s\n", colorRed, colorReset)
			fmt.Printf("\n%s⚠️  Please install Helium first.%s\n", colorYellow, colorReset)
			fmt.Printf("%s   Download from: https://helium.is/%s\n\n", colorBlue, colorReset)
			return err
		}
		fmt.Printf("%s✓ Found Helium at: %s%s\n", colorGreen, heliumVersionPath, colorReset)
	}

	var heliumWVPath string

	if runtime.GOOS == "linux" {
		manifestVersion, err := readWidevineVersion(chromeWVPath)
		if err != nil {
			return fmt.Errorf("failed to read Widevine version: %w", err)
		}

		userDataDir, err := findHeliumUserDataDir(log)
		if err != nil {
			fmt.Printf("\n%s✗ Could not find Helium user data directory.%s\n", colorRed, colorReset)
			fmt.Printf("%s  Please launch Helium at least once before running this tool.%s\n\n", colorYellow, colorReset)
			return err
		}

		heliumWVPath = filepath.Join(userDataDir, "WidevineCdm", manifestVersion)
		log.info("Using Linux component updater path: %s", heliumWVPath)
	} else {
		heliumWVPath = filepath.Join(heliumVersionPath, "WidevineCdm")
	}

	if opts.check {
		fmt.Printf("\n%s✅ Check complete. Fix can be applied.%s\n\n", colorBold+colorGreen, colorReset)
		return nil
	}

	if opts.dryRun {
		fmt.Printf("\n%s🔍 Dry run - no changes made.%s\n", colorBold+colorYellow, colorReset)
		fmt.Printf("%s   Would copy: %s%s\n", colorDim, chromeWVPath, colorReset)
		fmt.Printf("%s   To: %s%s\n\n", colorDim, heliumWVPath, colorReset)
		return nil
	}

	fmt.Print("Copying WidevineCdm from Chrome to Helium... ")

	if err := copyDir(chromeWVPath, heliumWVPath); err != nil {
		fmt.Printf("%s✗%s\n", colorRed, colorReset)
		return fmt.Errorf("failed to copy WidevineCdm: %w", err)
	}

	fmt.Printf("%s✓%s\n", colorGreen, colorReset)

	if runtime.GOOS == "linux" {
		hintFile := filepath.Join(filepath.Dir(heliumWVPath), "latest-component-updated-widevine-cdm")
		hintData := fmt.Sprintf(`{"Path":"%s"}`, heliumWVPath)
		if err := os.WriteFile(hintFile, []byte(hintData), 0644); err != nil {
			log.warn("Failed to write hint file: %v", err)
		}
		log.info("Wrote hint file: %s", hintFile)
	}

	if runtime.GOOS == "darwin" {
		fmt.Print("Re-signing Helium.app to include WidevineCdm... ")

		if err := resignHeliumApp(); err != nil {
			fmt.Printf("%s✗%s\n", colorRed, colorReset)
			fmt.Printf("\n%s⚠️  You may need to run with sudo, or manually run:%s\n", colorYellow, colorReset)
			fmt.Printf("%s   codesign --force --deep --sign - \"/Applications/Helium.app\"%s\n\n", colorDim, colorReset)
		} else {
			fmt.Printf("%s✓%s\n", colorGreen, colorReset)
		}
	}

	fmt.Printf("\n%s✅ Done! Restart Helium browser for DRM to work.%s\n\n", colorBold+colorGreen, colorReset)
	return nil
}
