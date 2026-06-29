package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func find7zPath() (string, error) {
	candidates := []string{"7z", "7z.exe"}

	for _, name := range candidates {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}

	commonPaths := []string{
		`C:\Program Files\7-Zip\7z.exe`,
		`C:\Program Files (x86)\7-Zip\7z.exe`,
	}

	for _, p := range commonPaths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", errors.New("7-Zip not found. Please install 7-Zip from https://www.7-zip.org/")
}

func extractArchive(archivePath, destDir string) error {
	sevenZip, err := find7zPath()
	if err != nil {
		return err
	}

	cmd := exec.Command(sevenZip, "x", archivePath, fmt.Sprintf("-o%s", destDir), "-y")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("7z extraction failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

func findWidevineInExtracted(baseDir string) (string, error) {
	var result string
	err := filepath.WalkDir(baseDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == "WidevineCdm" || d.Name() == "WidevineCdm.plugin") {
			result = path
			return filepath.SkipAll
		}
		return nil
	})

	if err != nil {
		return "", err
	}

	if result == "" {
		return "", errors.New("WidevineCdm not found in extracted files")
	}

	return result, nil
}
